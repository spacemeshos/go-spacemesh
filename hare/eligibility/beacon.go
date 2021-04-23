package eligibility

import (
	"crypto/sha256"
	"encoding/binary"
	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type addGet interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

// Beacon provides the value that is under consensus as defined by the hare.
type Beacon struct {
	beaconGetter    blocks.BeaconGetter
	confidenceParam uint64
	cache           addGet
	log.Log
}

// NewBeacon returns a new beacon
// patternProvider provides the contextually valid blocks.
// confidenceParam is the number of layers that the Beacon assumes for consensus view.
func NewBeacon(beaconGetter blocks.BeaconGetter, confidenceParam uint64, logger log.Log) *Beacon {
	c, e := lru.New(activesCacheSize)
	if e != nil {
		logger.Panic("could not create lru cache, err: %v", e)
	}
	return &Beacon{
		beaconGetter:    beaconGetter,
		confidenceParam: confidenceParam,
		cache:           c,
		Log:             logger,
	}
}

// Value returns the unpredictable and agreed value for the given layer
// Note: Value is concurrency-safe but not concurrency-optimized
func (b *Beacon) Value(layer types.LayerID) (uint32, error) {
	sl := safeLayer(layer, types.LayerID(b.confidenceParam))
	logger := b.WithFields(layer,
		log.FieldNamed("safe_layer", sl),
		log.FieldNamed("safe_epoch", sl.GetEpoch()))

	// check cache
	if val, exist := b.cache.Get(sl); exist {
		return val.(uint32), nil
	}

	// TODO: do we need a lock here?
	v := b.beaconGetter.GetBeacon(sl.GetEpoch())
	logger.With().Debug("raw beacon value for safe epoch", log.String("beacon_hex", util.Bytes2Hex(v)))

	// hash in the layer
	layerHash := sha256.Sum256(sl.Bytes())
	for i := range layerHash[:4] {
		v[i] ^= layerHash[i]
	}
	value := binary.LittleEndian.Uint32(v)
	logger.With().Debug("final beacon value for safe epoch and layer",
		log.String("beacon_hex", util.Bytes2Hex(v)),
		log.Uint32("beacon_dec", value))

	// update and return
	b.cache.Add(sl, value)
	return value, nil
}
