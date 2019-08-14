package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"hash/fnv"
	"sort"
)

const nilVal = 0

type patternProvider interface {
	// GetPatternId returns the pattern Id of the given Layer
	// the pattern Id is defined to be the hash of blocks in a Layer
	GetGoodPattern(layer types.LayerID) (map[types.BlockID]struct{}, error)
}

type beacon struct {
	// provides a value that is unpredictable and agreed (w.h.p.) by all honest
	patternProvider patternProvider
	confidenceParam uint64
}

func NewBeacon(patternProvider patternProvider, confidenceParam uint64) *beacon {
	return &beacon{
		patternProvider: patternProvider,
		confidenceParam: confidenceParam,
	}
}

// Value returns the unpredictable and agreed value for the given Layer
func (b *beacon) Value(layer types.LayerID) (uint32, error) {
	v, err := b.patternProvider.GetGoodPattern(safeLayer(layer, types.LayerID(b.confidenceParam)))
	if err != nil {
		log.Error("Could not get pattern Id: %v", err)
		return nilVal, err
	}

	return getBeaconFromSet(v), nil
}

func getBeaconFromSet(bids map[types.BlockID]struct{}) uint32 {
	keys := make([]types.BlockID, 0, len(bids))
	for k := range bids {
		keys = append(keys, k)
	}
	return getBeacon(keys)
}

func getBeacon(bids []types.BlockID) uint32 {
	sort.Slice(bids, func(i, j int) bool { return bids[i] < bids[j] })
	// calc
	h := fnv.New32()
	for i := 0; i < len(bids); i++ {
		h.Write(common.Uint32ToBytes(uint32(bids[i])))
	}
	// update
	sum := h.Sum32()
	return sum
}
