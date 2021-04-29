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
	// ContextuallyValidBlock returns the pattern ID of the given layer
	// the pattern ID is defined to be the hash of blocks in a layer
	LayerContextuallyValidBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error)
}

type addGet interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

// Beacon provides the value that is under consensus as defined by the hare.
type Beacon struct {
	// provides a value that is unpredictable and agreed (w.h.p.) by all honest
	patternProvider patternProvider
	confidenceParam uint64
	cache           addGet
	log.Log
}

// NewBeacon returns a new Beacon.
// patternProvider provides the contextually valid blocks.
// confidenceParam is the number of layers that the Beacon assumes for consensus view.
func NewBeacon(patternProvider patternProvider, confidenceParam uint64, lg log.Log) *Beacon {
	c, e := lru.New(activesCacheSize)
	if e != nil {
		lg.Panic("Could not create lru cache err=%v", e)
	}
	return &Beacon{
		patternProvider: patternProvider,
		confidenceParam: confidenceParam,
		cache:           c,
		Log:             lg,
	}
}

// Value returns the unpredictable and agreed value for the given layer
// Note: Value is concurrency-safe but not concurrency-optimized
func (b *Beacon) Value(layer types.LayerID) (uint32, error) {
	// TODO: this is a temporary hack, it will go away when https://github.com/spacemeshos/go-spacemesh/pull/2394 is merged
	var sl types.LayerID
	if layer <= types.GetEffectiveGenesis()+types.LayerID(b.confidenceParam) {
		sl = types.GetEffectiveGenesis()
	} else {
		sl = layer - types.LayerID(b.confidenceParam)
	}
	logger := b.Log.WithFields(layer, log.FieldNamed("sl_id", sl))

	// check cache
	if val, exist := b.cache.Get(sl); exist {
		return val.(uint32), nil
	}

	// note: multiple concurrent calls to LayerContextuallyValidBlocks and calcValue can be made
	// consider adding a lock if concurrency-optimized is important
	v, err := b.patternProvider.LayerContextuallyValidBlocks(sl)
	if err != nil {
		logger.With().Error("could not get pattern id", log.Err(err))
		return nilVal, errors.New("could not calculate beacon value")
	}

	// notify if there are no contextually valid blocks
	if len(v) == 0 {
		if types.EpochID(layer).IsGenesis() {
			logger.Info("hare beacon: zero contextually valid blocks in genesis layer (expected)")
		} else {
			logger.Warning("hare beacon: zero contextually valid blocks")
		}
	}

	// calculate
	value := calcValue(v)

	// update
	b.cache.Add(sl, value)

	return value, nil
}

// calculates the Beacon value from the set of ids
func calcValue(bids map[types.BlockID]struct{}) uint32 {
	keys := make([]types.BlockID, 0, len(bids))
	for k := range bids {
		keys = append(keys, k)
	}

	keys = types.SortBlockIDs(keys)

	// calc
	h := fnv.New32()
	for i := 0; i < len(keys); i++ {
		_, err := h.Write(keys[i].Bytes())
		if err != nil {
			log.Panic("Could not calculate Beacon value. Hash write error=%v", err)
		}
	}
	// update
	sum := h.Sum32()
	return sum
}
