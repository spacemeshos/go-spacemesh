package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errTargetEpochMismatch = errors.New("ATX target epoch and ballot publish epoch mismatch")
	errPublicKeyMismatch   = errors.New("ballot smesher key and ATX node key mismatch")
	errIncorrectCounter    = errors.New("proof counter larger than number of slots available")
	errInvalidProofsOrder  = errors.New("proofs are out of order")
	errIncorrectVRFSig     = errors.New("proof contains incorrect VRF signature")
	errIncorrectLayerIndex = errors.New("ballot has incorrect layer index")
	errIncorrectEligCount  = errors.New("ballot has incorrect eligibility count")
)

// Validator validates the eligibility of a Ballot.
// the validation focuses on eligibility only and assumes the Ballot to be valid otherwise.
type Validator struct {
	minActiveSetWeight uint64
	avgLayerSize       uint32
	layersPerEpoch     uint32
	tortoise           tortoiseProvider
	cdb                *datastore.CachedDB
	clock              layerClock
	beacons            system.BeaconCollector
	logger             log.Log
	vrfVerifier        vrfVerifier
	nonceFetcher       nonceFetcher
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
	avgLayerSize, layersPerEpoch uint32, minActiveSetWeight uint64, clock layerClock, tortoise tortoiseProvider, cdb *datastore.CachedDB, bc system.BeaconCollector, lg log.Log, vrfVerifier vrfVerifier, opts ...ValidatorOpt,
) *Validator {
	v := &Validator{
		minActiveSetWeight: minActiveSetWeight,
		avgLayerSize:       avgLayerSize,
		layersPerEpoch:     layersPerEpoch,
		tortoise:           tortoise,
		cdb:                cdb,
		nonceFetcher:       cdb,
		clock:              clock,
		beacons:            bc,
		logger:             lg,
		vrfVerifier:        vrfVerifier,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// CheckEligibility checks that a ballot is eligible in the layer that it specifies.
func (v *Validator) CheckEligibility(ctx context.Context, ballot *types.Ballot) (bool, error) {
	if len(ballot.EligibilityProofs) == 0 {
		return false, fmt.Errorf("empty eligibility list is invalid (ballot %s)", ballot.ID())
	}
	owned, err := v.cdb.GetAtxHeader(ballot.AtxID)
	if err != nil {
		return false, fmt.Errorf("failed to load atx %v: %w", ballot.AtxID, err)
	}
	if owned.TargetEpoch() != ballot.Layer.GetEpoch() {
		return false, fmt.Errorf("%w: ATX target epoch (%v), ballot publication epoch (%v)",
			errTargetEpochMismatch, owned.TargetEpoch(), ballot.Layer.GetEpoch())
	}
	if ballot.SmesherID != owned.NodeID {
		return false, fmt.Errorf("%w: public key (%v), ATX node key (%v)", errPublicKeyMismatch, ballot.SmesherID.String(), owned.NodeID)
	}
	nonce, err := v.nonceFetcher.VRFNonce(ballot.SmesherID, ballot.Layer.GetEpoch())
	if err != nil {
		return false, fmt.Errorf("no vrf nonce for %v in epoch %v: %w", ballot.SmesherID, ballot.Layer.GetEpoch(), err)
	}
	var data *types.EpochData
	if ballot.EpochData != nil && ballot.Layer.GetEpoch() == v.clock.CurrentLayer().GetEpoch() {
		var err error
		data, err = v.validateReference(ballot, owned)
		if err != nil {
			return false, err
		}
	} else {
		var err error
		data, err = v.validateSecondary(ballot, owned)
		if err != nil {
			return false, err
		}
	}
	for i, proof := range ballot.EligibilityProofs {
		if proof.J >= data.EligibilityCount {
			return false, fmt.Errorf("%w: proof counter (%d) numEligibleBallots (%d)",
				errIncorrectCounter, proof.J, data.EligibilityCount)
		}
		if i != 0 && proof.J <= ballot.EligibilityProofs[i-1].J {
			return false, fmt.Errorf("%w: %d <= %d", errInvalidProofsOrder, proof.J, ballot.EligibilityProofs[i-1].J)
		}
		if !v.vrfVerifier.Verify(ballot.SmesherID,
			MustSerializeVRFMessage(data.Beacon, ballot.Layer.GetEpoch(), nonce, proof.J), proof.Sig) {
			return false, fmt.Errorf("%w: beacon: %v, epoch: %v, counter: %v, vrfSig: %s",
				errIncorrectVRFSig, data.Beacon.ShortString(), ballot.Layer.GetEpoch(), proof.J, proof.Sig,
			)
		}
		if eligible := CalcEligibleLayer(ballot.Layer.GetEpoch(), v.layersPerEpoch, proof.Sig); ballot.Layer != eligible {
			return false, fmt.Errorf("%w: ballot layer (%v), eligible layer (%v)",
				errIncorrectLayerIndex, ballot.Layer, eligible)
		}
	}

	v.logger.WithContext(ctx).With().Debug("ballot eligibility verified",
		ballot.ID(),
		ballot.Layer,
		ballot.Layer.GetEpoch(),
		data.Beacon,
	)

	v.beacons.ReportBeaconFromBallot(ballot.Layer.GetEpoch(), ballot, data.Beacon,
		fixed.DivUint64(owned.GetWeight(), uint64(data.EligibilityCount)))
	return true, nil
}

// validateReference executed for reference ballots in latest epoch.
func (v *Validator) validateReference(ballot *types.Ballot, owned *types.ActivationTxHeader) (*types.EpochData, error) {
	if ballot.EpochData.Beacon == types.EmptyBeacon {
		return nil, fmt.Errorf("%w: ref ballot %v", errMissingBeacon, ballot.ID())
	}
	if len(ballot.ActiveSet) == 0 {
		return nil, fmt.Errorf("%w: ref ballot %v", errEmptyActiveSet, ballot.ID())
	}
	var totalWeight uint64
	for _, atxID := range ballot.ActiveSet {
		atx, err := v.cdb.GetAtxHeader(atxID)
		if err != nil {
			return nil, fmt.Errorf("atx in active set is missing %v: %w", atxID, err)
		}
		totalWeight += atx.GetWeight()
	}
	numEligibleSlots, err := GetNumEligibleSlots(owned.GetWeight(), v.minActiveSetWeight, totalWeight, v.avgLayerSize, v.layersPerEpoch)
	if err != nil {
		return nil, err
	}
	if ballot.EpochData.EligibilityCount != numEligibleSlots {
		return nil, fmt.Errorf("%w: expected %v, got: %v", errIncorrectEligCount, numEligibleSlots, ballot.EpochData.EligibilityCount)
	}
	return ballot.EpochData, nil
}

// validateSecondary executed for non-reference ballots in latest epoch and all ballots in past epochs.
func (v *Validator) validateSecondary(ballot *types.Ballot, owned *types.ActivationTxHeader) (*types.EpochData, error) {
	if ballot.RefBallot == types.EmptyBallotID {
		if ballot.EpochData == nil {
			return nil, fmt.Errorf("%w: ref ballot %v", errMissingEpochData, ballot.ID())
		}
		return ballot.EpochData, nil
	}
	refdata := v.tortoise.GetBallot(ballot.RefBallot)
	if refdata == nil {
		return nil, fmt.Errorf("ref ballot is missing %v", ballot.RefBallot)
	}
	if refdata.ATXID != ballot.AtxID {
		return nil, fmt.Errorf("ballot (%v/%v) should be sharing atx with a reference ballot (%v/%v)", ballot.ID(), ballot.AtxID, refdata.ID, refdata.ATXID)
	}
	if refdata.Smesher != ballot.SmesherID {
		return nil, fmt.Errorf("mismatched smesher id with refballot in ballot %v", ballot.ID())
	}
	if refdata.Layer.GetEpoch() != ballot.Layer.GetEpoch() {
		return nil, fmt.Errorf("ballot %v targets mismatched epoch %d", ballot.ID(), ballot.Layer.GetEpoch())
	}
	return &types.EpochData{Beacon: refdata.Beacon, EligibilityCount: refdata.Eligiblities}, nil
}
