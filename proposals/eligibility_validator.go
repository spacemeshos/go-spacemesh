package proposals

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errTargetEpochMismatch = errors.New("ATX target epoch and ballot publish epoch mismatch")
	errPublicKeyMismatch   = errors.New("ballot smesher key and ATX node key mismatch")
	errIncorrectCounter    = errors.New("proof counter larger than number of slots available")
	errInvalidProofsOrder  = errors.New("proofs are out of order")
	errIncorrectVRFSig     = errors.New("proof contains incorrect VRF signature")
	errIncorrectLayerIndex = errors.New("ballot has incorrect layer index")
)

// Validator validates the eligibility of a Ballot.
// the validation focuses on eligibility only and assumes the Ballot to be valid otherwise.
type Validator struct {
	avgLayerSize   uint32
	layersPerEpoch uint32
	cdb            *datastore.CachedDB
	mesh           meshProvider
	beacons        system.BeaconCollector
	logger         log.Log
	vrfVerifier    vrfVerifier
	nonceFetcher   nonceFetcher
}

type defaultFetcher struct {
	cdb *datastore.CachedDB
}

func (f defaultFetcher) VRFNonce(nodeID types.NodeID, epoch types.EpochID) (types.VRFPostIndex, error) {
	return atxs.VRFNonce(f.cdb, nodeID, epoch)
}

// ValidatorOpt for configuring Validator.
type ValidatorOpt func(h *Validator)

func WithNonceFetcher(nf nonceFetcher) ValidatorOpt {
	return func(h *Validator) {
		h.nonceFetcher = nf
	}
}

// NewEligibilityValidator returns a new EligibilityValidator.
func NewEligibilityValidator(
	avgLayerSize, layersPerEpoch uint32, cdb *datastore.CachedDB, bc system.BeaconCollector, m meshProvider, lg log.Log, vrfVerifier vrfVerifier, opts ...ValidatorOpt,
) *Validator {
	v := &Validator{
		avgLayerSize:   avgLayerSize,
		layersPerEpoch: layersPerEpoch,
		cdb:            cdb,
		mesh:           m,
		beacons:        bc,
		logger:         lg,
		vrfVerifier:    vrfVerifier,
	}
	for _, opt := range opts {
		opt(v)
	}

	if v.nonceFetcher == nil {
		v.nonceFetcher = defaultFetcher{cdb: cdb}
	}

	return v
}

// CheckEligibility checks that a ballot is eligible in the layer that it specifies.
func (v *Validator) CheckEligibility(ctx context.Context, ballot *types.Ballot) (bool, error) {
	var (
		atxWeight, totalWeight uint64
		err                    error
		refBallot              = ballot
		epoch                  = ballot.Layer.GetEpoch()
	)
	if len(ballot.EligibilityProofs) == 0 {
		return false, fmt.Errorf("empty eligibility list is invalid (ballot %s)", ballot.ID())
	}

	if ballot.RefBallot != types.EmptyBallotID {
		if refBallot, err = ballots.Get(v.cdb, ballot.RefBallot); err != nil {
			return false, fmt.Errorf("get ref ballot %v: %w", ballot.RefBallot, err)
		}
	}
	if refBallot.EpochData == nil {
		return false, fmt.Errorf("%w: ref ballot %v", errMissingEpochData, refBallot.ID())
	}
	if refBallot.AtxID != ballot.AtxID {
		return false, fmt.Errorf("ballot (%v/%v) should be sharing atx with a reference ballot (%v/%v)", ballot.ID(), ballot.AtxID, refBallot.ID(), refBallot.AtxID)
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
	var owned *types.ActivationTxHeader
	for _, atxID := range activeSets {
		atx, err := v.cdb.GetAtxHeader(atxID)
		if err != nil {
			return false, fmt.Errorf("get ATX header %v: %w", atxID, err)
		}
		totalWeight += atx.GetWeight()
		if atxID == ballot.AtxID {
			owned = atx
		}
	}
	if owned == nil {
		return false, fmt.Errorf("atx %v from ballot %v (refballot %v) is not included into the active set", ballot.AtxID, ballot.ID(), refBallot.ID())
	}
	if targetEpoch := owned.TargetEpoch(); targetEpoch != epoch {
		return false, fmt.Errorf("%w: ATX target epoch (%v), ballot publication epoch (%v)",
			errTargetEpochMismatch, targetEpoch, epoch)
	}
	if pub := ballot.SmesherID(); !bytes.Equal(owned.NodeID.Bytes(), pub.Bytes()) {
		return false, fmt.Errorf("%w: public key (%v), ATX node key (%v)", errPublicKeyMismatch, pub.String(), owned.NodeID)
	}

	atxWeight = owned.GetWeight()

	numEligibleSlots, err := GetNumEligibleSlots(atxWeight, totalWeight, v.avgLayerSize, v.layersPerEpoch)
	if err != nil {
		return false, err
	}

	var (
		last    uint32
		isFirst = true
	)

	nonce, err := v.nonceFetcher.VRFNonce(ballot.SmesherID(), epoch)
	if err != nil {
		return false, err
	}
	for _, proof := range ballot.EligibilityProofs {
		counter := proof.J
		if counter >= numEligibleSlots {
			return false, fmt.Errorf("%w: proof counter (%d) numEligibleBallots (%d), totalWeight (%v)",
				errIncorrectCounter, counter, numEligibleSlots, totalWeight)
		}
		if isFirst {
			isFirst = false
		} else if counter <= last {
			return false, fmt.Errorf("%w: %d <= %d", errInvalidProofsOrder, counter, last)
		}
		last = counter

		message, err := SerializeVRFMessage(beacon, epoch, nonce, counter)
		if err != nil {
			return false, err
		}
		vrfSig := proof.Sig

		beaconStr := beacon.ShortString()
		if !v.vrfVerifier.Verify(owned.NodeID, message, vrfSig) {
			return false, fmt.Errorf("%w: beacon: %v, epoch: %v, counter: %v, vrfSig: %v",
				errIncorrectVRFSig, beaconStr, epoch, counter, types.BytesToHash(vrfSig).ShortString())
		}

		eligibleLayer := CalcEligibleLayer(epoch, v.layersPerEpoch, vrfSig)
		if ballot.Layer != eligibleLayer {
			return false, fmt.Errorf("%w: ballot layer (%v), eligible layer (%v)",
				errIncorrectLayerIndex, ballot.Layer, eligibleLayer)
		}
	}

	v.logger.WithContext(ctx).With().Debug("ballot eligibility verified",
		ballot.ID(),
		ballot.Layer,
		epoch,
		beacon,
	)

	weightPer := fixed.DivUint64(atxWeight, uint64(numEligibleSlots))
	v.beacons.ReportBeaconFromBallot(epoch, ballot, beacon, weightPer)
	return true, nil
}
