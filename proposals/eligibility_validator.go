package proposals

import (
	"context"
	"fmt"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner/minweight"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/system"
)

var ErrBoundary = fmt.Errorf("validator: eligibility count doesn't match expected bondaries")

// Validator validates the eligibility of a Ballot.
// the validation focuses on eligibility only and assumes the Ballot to be valid otherwise.
type Validator struct {
	minActiveSetWeight []types.EpochMinimalActiveWeight
	avgLayerSize       uint32
	layersPerEpoch     uint32
	tortoise           tortoiseProvider
	atxsdata           *atxsdata.Data
	clock              layerClock
	beacons            system.BeaconCollector
	logger             log.Log
	vrfVerifier        vrfVerifier
	validateBoundaries types.EpochID
}

// ValidatorOpt for configuring Validator.
type ValidatorOpt func(h *Validator)

// NewEligibilityValidator returns a new EligibilityValidator.
func NewEligibilityValidator(
	avgLayerSize, layersPerEpoch uint32,
	minActiveSetWeight []types.EpochMinimalActiveWeight,
	validateBoundaries types.EpochID,
	clock layerClock,
	tortoise tortoiseProvider,
	atxsdata *atxsdata.Data,
	bc system.BeaconCollector,
	lg log.Log,
	vrfVerifier vrfVerifier,
	opts ...ValidatorOpt,
) *Validator {
	v := &Validator{
		minActiveSetWeight: minActiveSetWeight,
		avgLayerSize:       avgLayerSize,
		layersPerEpoch:     layersPerEpoch,
		validateBoundaries: validateBoundaries,
		tortoise:           tortoise,
		atxsdata:           atxsdata,
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
func (v *Validator) CheckEligibility(ctx context.Context, ballot *types.Ballot, weight uint64) error {
	if len(ballot.EligibilityProofs) == 0 {
		return fmt.Errorf(
			"%w: empty eligibility list is invalid (ballot %s)",
			pubsub.ErrValidationReject,
			ballot.ID(),
		)
	}
	atx := v.atxsdata.Get(ballot.Layer.GetEpoch(), ballot.AtxID)
	if atx == nil {
		return fmt.Errorf(
			"failed to load atx from cache with epoch %d %s",
			ballot.Layer.GetEpoch(),
			ballot.AtxID.ShortString(),
		)
	}
	if atx.Node != ballot.SmesherID {
		return fmt.Errorf(
			"%w: referenced atx %s belongs to a different smesher %s",
			pubsub.ErrValidationReject,
			atx.Node.ShortString(),
			ballot.SmesherID.ShortString(),
		)
	}
	var (
		data *types.EpochData
		err  error
	)
	if ballot.EpochData != nil && v.validateBoundaries >= ballot.Layer.GetEpoch() {
		data, err = v.validateReferenceBoundaries(ballot, atx.Weight, weight)
	} else if ballot.EpochData != nil && ballot.Layer.GetEpoch() == v.clock.CurrentLayer().GetEpoch() {
		data, err = v.validateReference(ballot, atx.Weight, weight)
	} else {
		data, err = v.validateSecondary(ballot)
	}
	if err != nil {
		return err
	}
	for i, proof := range ballot.EligibilityProofs {
		if proof.J >= data.EligibilityCount {
			return fmt.Errorf("%w: proof counter larger than number of slots (%d) numEligibleBallots (%d)",
				pubsub.ErrValidationReject, proof.J, data.EligibilityCount)
		}
		if i != 0 && proof.J <= ballot.EligibilityProofs[i-1].J {
			return fmt.Errorf(
				"%w: proofs are out of order: %d <= %d",
				pubsub.ErrValidationReject,
				proof.J,
				ballot.EligibilityProofs[i-1].J,
			)
		}
		if !v.vrfVerifier.Verify(ballot.SmesherID,
			MustSerializeVRFMessage(data.Beacon, ballot.Layer.GetEpoch(), atx.Nonce, proof.J), proof.Sig) {
			return fmt.Errorf(
				"%w: proof contains incorrect VRF signature. beacon: %v, epoch: %v, counter: %v, vrfSig: %s",
				pubsub.ErrValidationReject,
				data.Beacon.ShortString(),
				ballot.Layer.GetEpoch(),
				proof.J,
				proof.Sig,
			)
		}
		if eligible := CalcEligibleLayer(ballot.Layer.GetEpoch(), v.layersPerEpoch, proof.Sig); ballot.Layer != eligible {
			return fmt.Errorf("%w: ballot has incorrect layer index. ballot layer (%v), eligible layer (%v)",
				pubsub.ErrValidationReject, ballot.Layer, eligible)
		}
	}

	v.logger.WithContext(ctx).With().Debug("ballot eligibility verified",
		ballot.ID(),
		ballot.Layer,
		ballot.Layer.GetEpoch(),
		data.Beacon,
	)

	v.beacons.ReportBeaconFromBallot(ballot.Layer.GetEpoch(), ballot, data.Beacon,
		fixed.DivUint64(atx.Weight, uint64(data.EligibilityCount)))
	return nil
}

// validateReference executed for reference ballots in latest epoch.
func (v *Validator) validateReference(
	ballot *types.Ballot,
	weight, totalWeight uint64,
) (*types.EpochData, error) {
	if ballot.EpochData.Beacon == types.EmptyBeacon {
		return nil, fmt.Errorf("%w: beacon is missing in ref ballot %v", pubsub.ErrValidationReject, ballot.ID())
	}
	if totalWeight == 0 {
		return nil, fmt.Errorf("%w: empty active set in ref ballot %v", pubsub.ErrValidationReject, ballot.ID())
	}
	numEligibleSlots, err := GetNumEligibleSlots(
		weight,
		minweight.Select(ballot.Layer.GetEpoch(), v.minActiveSetWeight),
		totalWeight,
		v.avgLayerSize,
		v.layersPerEpoch,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", pubsub.ErrValidationReject, err)
	}
	if ballot.EpochData.EligibilityCount != numEligibleSlots {
		return nil, fmt.Errorf(
			"%w: ballot has incorrect eligibility count expected %v, got: %v",
			pubsub.ErrValidationReject,
			numEligibleSlots,
			ballot.EpochData.EligibilityCount,
		)
	}
	return ballot.EpochData, nil
}

// validateSecondary executed for non-reference ballots in latest epoch and all ballots in past epochs.
func (v *Validator) validateSecondary(ballot *types.Ballot) (*types.EpochData, error) {
	if ballot.RefBallot == types.EmptyBallotID {
		if ballot.EpochData == nil {
			return nil, fmt.Errorf(
				"%w: epoch data is missing in ref ballot %v",
				pubsub.ErrValidationReject,
				ballot.ID(),
			)
		}
		return ballot.EpochData, nil
	}
	refdata := v.tortoise.GetBallot(ballot.RefBallot)
	if refdata == nil {
		return nil, fmt.Errorf("ref ballot is missing %v", ballot.RefBallot)
	}
	if refdata.ATXID != ballot.AtxID {
		return nil, fmt.Errorf(
			"%w: ballot (%v/%v) should be sharing atx with a reference ballot (%v/%v)",
			pubsub.ErrValidationReject,
			ballot.ID(),
			ballot.AtxID,
			refdata.ID,
			refdata.ATXID,
		)
	}
	if refdata.Smesher != ballot.SmesherID {
		return nil, fmt.Errorf(
			"%w: mismatched smesher id with refballot in ballot %v",
			pubsub.ErrValidationReject,
			ballot.ID(),
		)
	}
	if refdata.Layer.GetEpoch() != ballot.Layer.GetEpoch() {
		return nil, fmt.Errorf(
			"%w: ballot %v targets mismatched epoch %d",
			pubsub.ErrValidationReject,
			ballot.ID(),
			ballot.Layer.GetEpoch(),
		)
	}
	return &types.EpochData{Beacon: refdata.Beacon, EligibilityCount: refdata.Eligiblities}, nil
}

func (v *Validator) validateReferenceBoundaries(
	ballot *types.Ballot,
	atxWeight, activeSetWeight uint64,
) (*types.EpochData, error) {
	if ballot.EpochData.Beacon == types.EmptyBeacon {
		return nil, fmt.Errorf("%w: beacon is missing in ref ballot %v", pubsub.ErrValidationReject, ballot.ID())
	}
	// this error is possible only due to programmer mistake, for example if NonDecreasingWeight is returned as zero
	// atx for this ballot actually stored locally
	if activeSetWeight == 0 {
		return nil, fmt.Errorf("zero local weight. failed to validate ballot %v", ballot.ID())
	}
	minWeight := minweight.Select(ballot.Layer.GetEpoch(), v.minActiveSetWeight)
	upperBoundary := MustGetNumEligibleSlots(
		atxWeight,
		minWeight,
		minWeight,
		v.avgLayerSize,
		v.layersPerEpoch,
	)
	lowerBoundary := MustGetNumEligibleSlots(
		atxWeight,
		activeSetWeight,
		activeSetWeight,
		v.avgLayerSize,
		v.layersPerEpoch,
	)
	if ballot.EpochData.EligibilityCount > upperBoundary {
		// this can only be the case if the other node is misconfigured or intentionally dos'ing network.
		// safe to reject
		return nil, fmt.Errorf(
			"%w %w: min weight %d. lower boundary %v. ballot count %d",
			pubsub.ErrValidationReject,
			ErrBoundary,
			minWeight,
			lowerBoundary,
			ballot.EpochData.EligibilityCount,
		)
	} else if ballot.EpochData.EligibilityCount < lowerBoundary {
		// in this case we should not reject it, as it will disconnect such peer from us.
		// but we in fact might be missing atxs and instead should run consistency check with that peer.
		return nil, fmt.Errorf(
			"%w: local weight %d. upper boundary %v. ballot count %d",
			ErrBoundary,
			activeSetWeight,
			upperBoundary,
			ballot.EpochData.EligibilityCount,
		)
	}
	return ballot.EpochData, nil
}
