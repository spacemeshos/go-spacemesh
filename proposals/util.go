package proposals

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

var (
	// ErrZeroTotalWeight is returned when zero total epoch weight is used when calculating eligible slots.
	ErrZeroTotalWeight = errors.New("zero total weight not allowed")

	errBadBallotData = errors.New("bad ballot data")
)

// CalcEligibleLayer calculates the eligible layer from the VRF signature.
func CalcEligibleLayer(epochNumber types.EpochID, layersPerEpoch uint32, vrfSig []byte) types.LayerID {
	vrfInteger := util.BytesToUint64(vrfSig)
	eligibleLayerOffset := vrfInteger % uint64(layersPerEpoch)
	return epochNumber.FirstLayer().Add(uint32(eligibleLayerOffset))
}

// GetNumEligibleSlots calculates the number of eligible slots for a smesher in an epoch.
func GetNumEligibleSlots(weight, totalWeight uint64, committeeSize uint32, layersPerEpoch uint32) (uint32, error) {
	if totalWeight == 0 {
		return 0, ErrZeroTotalWeight
	}
	numberOfEligibleBlocks := weight * uint64(committeeSize) * uint64(layersPerEpoch) / totalWeight // TODO: ensure no overflow
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return uint32(numberOfEligibleBlocks), nil
}

//go:generate scalegen -types VrfMessage

// VrfMessage is a verification message.
type VrfMessage struct {
	Beacon  types.Beacon
	Epoch   types.EpochID
	Counter uint32
}

// SerializeVRFMessage serializes a message for generating/verifying a VRF signature.
func SerializeVRFMessage(beacon types.Beacon, epoch types.EpochID, counter uint32) ([]byte, error) {
	m := VrfMessage{
		Beacon:  beacon,
		Epoch:   epoch,
		Counter: counter,
	}
	serialized, err := codec.Encode(&m)
	if err != nil {
		return nil, fmt.Errorf("serialize vrf message: %w", err)
	}
	return serialized, nil
}

// ComputeWeightPerEligibility computes the ballot weight per eligibility w.r.t the active set recorded in its reference ballot.
func ComputeWeightPerEligibility(
	cdb *datastore.CachedDB,
	ballot *types.Ballot,
	layerSize,
	layersPerEpoch uint32,
) (util.Weight, error) {
	var (
		refBallot        = ballot
		atx              *types.ActivationTx
		err              error
		total, atxWeight uint64
	)
	if ballot.EpochData == nil {
		if ballot.RefBallot == types.EmptyBallotID {
			return util.Weight{}, fmt.Errorf("%w: empty ref ballot but no epoch data %s", errBadBallotData, ballot.ID())
		}
		refBallot, err = ballots.Get(cdb, ballot.RefBallot)
		if err != nil {
			return util.Weight{}, fmt.Errorf("%w: missing ref ballot %s (for %s)", err, ballot.RefBallot, ballot.ID())
		}
	}
	for _, atxID := range refBallot.EpochData.ActiveSet {
		atx, err = cdb.GetAtxByID(atxID)
		if err != nil {
			return util.Weight{}, fmt.Errorf("%w: missing atx %s in active set of %s (for %s)", err, atxID, refBallot.ID(), ballot.ID())
		}
		weight := atx.GetWeight()
		total += weight
		if atxID == ballot.AtxID {
			atxWeight = weight
		}
	}
	expNumSlots, err := GetNumEligibleSlots(atxWeight, total, layerSize, layersPerEpoch)
	if err != nil {
		return util.Weight{}, fmt.Errorf("failed to compute num eligibility for atx %s: %w", ballot.AtxID, err)
	}
	return util.WeightFromUint64(atxWeight).Div(util.WeightFromUint64(uint64(expNumSlots))), nil
}
