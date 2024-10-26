package proposals

import (
	"context"
	"fmt"

	"github.com/spacemeshos/fixed"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/miner/minweight"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Validator validates the eligibility of a Ballot.
// The validation focuses on eligibility only and assumes the Ballot to be valid otherwise.
type Validator struct {
	minActiveSetWeight []types.EpochMinimalActiveWeight
	avgLayerSize       uint32
	layersPerEpoch     uint32
	tortoise           tortoiseProvider
	atxsdata           *atxsdata.Data
	clock              layerClock
	beacons            system.BeaconCollector
	logger             *zap.Logger
	vrfVerifier        vrfVerifier
}

// ValidatorOpt for configuring Validator.
type ValidatorOpt func(h *Validator)

// NewEligibilityValidator returns a new EligibilityValidator.
func NewEligibilityValidator(
	avgLayerSize, layersPerEpoch uint32,
	minActiveSetWeight []types.EpochMinimalActiveWeight,
	clock layerClock,
	tortoise tortoiseProvider,
	atxsdata *atxsdata.Data,
	bc system.BeaconCollector,
	lg *zap.Logger,
	vrfVerifier vrfVerifier,
	opts ...ValidatorOpt,
) *Validator {
	v := &Validator{
		minActiveSetWeight: minActiveSetWeight,
		avgLayerSize:       avgLayerSize,
		layersPerEpoch:     layersPerEpoch,
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
	if ballot.EpochData != nil && ballot.Layer.GetEpoch() == v.clock.CurrentLayer().GetEpoch() {
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
		msg := MustSerializeVRFMessage(data.Beacon, ballot.Layer.GetEpoch(), atx.Nonce, proof.J)
		if !v.vrfVerifier.Verify(ballot.SmesherID, msg, proof.Sig) {
			return fmt.Errorf(
				"%w: proof contains incorrect VRF signature: "+
					"ballot: %s, smesher: %s, beacon: %s, epoch: %v, counter: %v, vrfSig: %s",
				fetch.ErrIgnore,
				ballot.ID(),
				ballot.SmesherID.ShortString(),
				data.Beacon,
				ballot.Layer.GetEpoch(),
				proof.J,
				proof.Sig,
			)
		}
		eligible := CalcEligibleLayer(ballot.Layer.GetEpoch(), v.layersPerEpoch, proof.Sig)
		if ballot.Layer != eligible {
			return fmt.Errorf(
				"%w: ballot has incorrect layer index. ballot layer (%v), eligible layer (%v)",
				pubsub.ErrValidationReject,
				ballot.Layer,
				eligible,
			)
		}
	}

	v.logger.Debug("ballot eligibility verified",
		zap.Stringer("id", ballot.ID()),
		zap.Uint32("layer", ballot.Layer.Uint32()),
		zap.Uint32("layer", ballot.Layer.GetEpoch().Uint32()),
		zap.Stringer("beacon", data.Beacon),
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
	return &types.EpochData{Beacon: refdata.Beacon, EligibilityCount: refdata.Eligibilities}, nil
}
