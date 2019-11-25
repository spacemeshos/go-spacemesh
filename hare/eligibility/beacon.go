package eligibility

import (
	"errors"
	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"hash/fnv"
)

const nilVal = 0

type patternProvider interface {
	// GetPatternId returns the pattern Id of the given Layer
	// the pattern Id is defined to be the hash of blocks in a Layer
	ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error)
}

type addGet interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

type beacon struct {
	// provides a value that is unpredictable and agreed (w.h.p.) by all honest
	patternProvider patternProvider
	confidenceParam uint64
	cache           addGet
}

// NewBeacon returns a new beacon.
// patternProvider provides the contextually valid blocks.
// confidenceParam is the number of layers that the beacon assumes for consensus view.
func NewBeacon(patternProvider patternProvider, confidenceParam uint64) *beacon {
	c, e := lru.New(cacheSize)
	if e != nil {
		log.Panic("Could not create lru cache err=%v", e)
	}
	return &beacon{
		patternProvider: patternProvider,
		confidenceParam: confidenceParam,
		cache:           c,
	}
}

// Value returns the unpredictable and agreed value for the given Layer
func (b *beacon) Value(layer types.LayerID) (uint32, error) {
	sl := safeLayer(layer, types.LayerID(b.confidenceParam))

	// check cache
	if val, exist := b.cache.Get(sl); exist {
		return val.(uint32), nil
	}

	v, err := b.patternProvider.ContextuallyValidBlock(sl)
	if err != nil {
		log.With().Error("Could not get pattern Id",
			log.Err(err), log.LayerId(uint64(layer)), log.Uint64("sl_id", uint64(sl)))
		return nilVal, errors.New("could not calc beacon value")
	}

	// calculate
	value := calcValue(v)

	// update
	b.cache.Add(sl, value)

	return value, nil
}

// calculates the beacon value from the set of ids
func calcValue(bids map[types.BlockID]struct{}) uint32 {
	keys := make([]types.BlockID, 0, len(bids))
	for k := range bids {
		keys = append(keys, k)
	}

	keys = types.SortBlockIds(keys)

	// calc
	h := fnv.New32()
	for i := 0; i < len(keys); i++ {
		_, err := h.Write(keys[i].ToBytes())
		if err != nil {
			log.Panic("Could not calculate beacon value. Hash write error=%v", err)
		}
	}
	// update
	sum := h.Sum32()
	return sum
}
