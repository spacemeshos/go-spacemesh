package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errTargetEpochMismatch = errors.New("ATX target epoch and ballot publish epoch mismatch")
	errPublicKeyMismatch   = errors.New("ballot smesher key and ATX node key mismatch")
	errIncorrectCounter    = errors.New("proof counter larger than number of slots available")
	errIncorrectVRFSig     = errors.New("proof contains incorrect VRF signature")
	errIncorrectLayerIndex = errors.New("ballot has incorrect layer index")
)

// Validator validates the eligibility of a Ballot.
// the validation focuses on eligibility only and assumes the Ballot to be valid otherwise.
type Validator struct {
	avgLayerSize   uint32
	layersPerEpoch uint32
	atxDB          atxDB
	mesh           meshDB
	beacons        system.BeaconCollector
	logger         log.Log
}

// NewEligibilityValidator returns a new EligibilityValidator.
func NewEligibilityValidator(
	avgLayerSize, layersPerEpoch uint32, db atxDB, bc system.BeaconCollector, m meshDB, lg log.Log) *Validator {
	return &Validator{
		avgLayerSize:   avgLayerSize,
		layersPerEpoch: layersPerEpoch,
		atxDB:          db,
		mesh:           m,
		beacons:        bc,
		logger:         lg,
	}
}

// CheckEligibility checks that a ballot is eligible in the layer that it specifies.
func (v *Validator) CheckEligibility(ctx context.Context, ballot *types.Ballot) (bool, error) {
	var (
		weight, totalWeight uint64
		err                 error
		refBallot           = ballot
		epoch               = ballot.LayerIndex.GetEpoch()
	)

	if ballot.RefBallot != types.EmptyBallotID {
		if refBallot, err = v.mesh.GetBallot(ballot.RefBallot); err != nil {
			return false, fmt.Errorf("get ref ballot %v: %w", ballot.RefBallot, err)
		}
	}
	if refBallot.EpochData == nil {
		return false, fmt.Errorf("%w: ref ballot %v", errMissingEpochData, refBallot.ID())
	}

	beacon := refBallot.EpochData.Beacon
	if beacon == types.EmptyBeacon {
		return false, fmt.Errorf("%w: ref ballot %v", errMissingBeacon, refBallot.ID())
	}

	activeSets := refBallot.EpochData.ActiveSet
	if len(activeSets) == 0 {
		return false, fmt.Errorf("%w: ref ballot %v", errEmptyActiveSet, refBallot.ID())
	}

	// todo: optimize by using reference to active set size and cache active set size to not load all atxsIDs from db
	for _, atxID := range activeSets {
		atx, err := v.atxDB.GetAtxHeader(atxID)
		if err != nil {
			return false, fmt.Errorf("get ATX header: %w", err)
		}
		totalWeight += atx.GetWeight()
	}

	atx, err := v.getBallotATX(ctx, ballot)
	if err != nil {
		return false, err
	}
	weight = atx.GetWeight()
	vrfPubkey := atx.NodeID.VRFPublicKey

	numEligibleBallots, err := GetNumEligibleSlots(weight, totalWeight, v.avgLayerSize, v.layersPerEpoch)
	if err != nil {
		return false, err
	}

	counter := ballot.EligibilityProof.J
	if counter >= numEligibleBallots {
		return false, fmt.Errorf("%w: proof counter (%d) numEligibleBallots (%d), totalWeight (%v)",
			errIncorrectCounter, counter, numEligibleBallots, totalWeight)
	}

	message, err := SerializeVRFMessage(beacon, epoch, counter)
	if err != nil {
		return false, err
	}
	vrfSig := ballot.EligibilityProof.Sig

	beaconStr := beacon.ShortString()
	if !signing.VRFVerify(vrfPubkey, message, vrfSig) {
		return false, fmt.Errorf("%w: beacon: %v, epoch: %v, counter: %v, vrfSig: %v",
			errIncorrectVRFSig, beaconStr, epoch, counter, types.BytesToHash(vrfSig).ShortString())
	}

	eligibleLayer := CalcEligibleLayer(epoch, v.layersPerEpoch, vrfSig)
	if ballot.LayerIndex != eligibleLayer {
		return false, fmt.Errorf("%w: ballot layer (%v), eligible layer (%v)",
			errIncorrectLayerIndex, ballot.LayerIndex, eligibleLayer)
	}

	v.logger.WithContext(ctx).With().Info("ballot eligibility verified",
		ballot.ID(),
		ballot.LayerIndex,
		epoch,
		beacon,
		log.Uint32("counter", counter))

	v.beacons.ReportBeaconFromBallot(epoch, ballot.ID(), beacon, weight)
	return true, nil
}

func (v Validator) getBallotATX(ctx context.Context, ballot *types.Ballot) (*types.ActivationTxHeader, error) {
	if ballot.AtxID == *types.EmptyATXID {
		v.logger.WithContext(ctx).Panic("empty ATXID in ballot")
	}

	epoch := ballot.LayerIndex.GetEpoch()
	atx, err := v.atxDB.GetAtxHeader(ballot.AtxID)
	if err != nil {
		return nil, fmt.Errorf("get ballot ATX %v epoch %v: %w", ballot.AtxID.ShortString(), epoch, err)
	}
	if targetEpoch := atx.PubLayerID.GetEpoch() + 1; targetEpoch != epoch {
		return nil, fmt.Errorf("%w: ATX target epoch (%v), ballot publication epoch (%v)",
			errTargetEpochMismatch, targetEpoch, epoch)
	}
	if pubString := ballot.SmesherID().String(); atx.NodeID.Key != pubString {
		return nil, fmt.Errorf("%w: public key (%v), ATX node key (%v)", errPublicKeyMismatch, pubString, atx.NodeID.Key)
	}
	return atx, nil
}
