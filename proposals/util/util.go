package util

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

var (
	// ErrZeroTotalWeight is returned when zero total epoch weight is used when calculating eligible slots.
	ErrZeroTotalWeight = errors.New("zero total weight not allowed")

	ErrBadBallotData = errors.New("bad ballot data")
)

// NOTE(dshulyak) this was moved into a separate module to disentangle dependencies
// between tortoise and proposals. tests were kept in the proposals/util_test.go
// i will refactor them in a followup

// CalcEligibleLayer calculates the eligible layer from the VRF signature.
func CalcEligibleLayer(epochNumber types.EpochID, layersPerEpoch uint32, vrfSig types.VrfSignature) types.LayerID {
	vrfInteger := binary.LittleEndian.Uint64(vrfSig[:])
	eligibleLayerOffset := vrfInteger % uint64(layersPerEpoch)
	return epochNumber.FirstLayer().Add(uint32(eligibleLayerOffset))
}

func maxWeight(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// GetNumEligibleSlots calculates the number of eligible slots for a smesher in an epoch.
func GetNumEligibleSlots(weight, minWeight, totalWeight uint64, committeeSize uint32, layersPerEpoch uint32) (uint32, error) {
	if totalWeight == 0 {
		return 0, ErrZeroTotalWeight
	}
	numberOfEligibleBlocks := weight * uint64(committeeSize) * uint64(layersPerEpoch) / maxWeight(minWeight, totalWeight) // TODO: ensure no overflow
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return uint32(numberOfEligibleBlocks), nil
}

// ComputeWeightPerEligibility computes the ballot weight per eligibility w.r.t the active set recorded in its reference ballot.
func ComputeWeightPerEligibility(
	cdb *datastore.CachedDB,
	ballot *types.Ballot,
	layerSize,
	layersPerEpoch uint32,
) (*big.Rat, error) {
	var (
		refBallot = ballot
		hdr       *types.ActivationTxHeader
		err       error
		atxWeight uint64
	)
	if ballot.EpochData == nil {
		if ballot.RefBallot == types.EmptyBallotID {
			return nil, fmt.Errorf("%w: empty ref ballot but no epoch data %s", ErrBadBallotData, ballot.ID())
		}
		refBallot, err = ballots.Get(cdb, ballot.RefBallot)
		if err != nil {
			return nil, fmt.Errorf("%w: missing ref ballot %s (for %s)", err, ballot.RefBallot, ballot.ID())
		}
	}
	if len(refBallot.ActiveSet) == 0 {
		return nil, fmt.Errorf("ref ballot missing active set %s (for %s)", ballot.RefBallot, ballot.ID())
	}
	if refBallot.EpochData == nil {
		return nil, fmt.Errorf("epoch data is nil on ballot %d/%s", refBallot.Layer, refBallot.ID())
	}
	if refBallot.EpochData.EligibilityCount == 0 {
		return nil, fmt.Errorf("eligibility count is 0 on ballot %d/%s", refBallot.Layer, refBallot.ID())
	}
	for _, atxID := range refBallot.ActiveSet {
		hdr, err = cdb.GetAtxHeader(atxID)
		if err != nil {
			return nil, fmt.Errorf("%w: missing atx %s in active set of %s (for %s)", err, atxID, refBallot.ID(), ballot.ID())
		}
		if atxID == ballot.AtxID {
			atxWeight = hdr.GetWeight()
			break
		}
	}
	if atxWeight == 0 {
		return nil, fmt.Errorf("atx id %v is not found in the active set of the reference ballot %v with atxid %v", ballot.AtxID, refBallot.ID(), refBallot.AtxID)
	}
	return new(big.Rat).SetFrac(
		new(big.Int).SetUint64(atxWeight),
		new(big.Int).SetUint64(uint64(refBallot.EpochData.EligibilityCount)),
	), nil
}
