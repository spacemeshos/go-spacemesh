package timesync

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// LayerConverter is a converter between time to layer ID struct.
type LayerConverter struct {
	duration time.Duration // the layer duration, assumed to be > 0
	genesis  time.Time     // the genesis time
}

// TimeToLayer returns the layer of the provided time.
func (lc LayerConverter) TimeToLayer(t time.Time) types.LayerID {
	if t.Before(lc.genesis) { // the genesis is in the future
		return 0
	}
	return types.NewLayerID(uint32(t.Sub(lc.genesis) / lc.duration))
}

// LayerToTime returns the time of the provided layer.
func (lc LayerConverter) LayerToTime(id types.LayerID) time.Time {
	return lc.genesis.Add(time.Duration(id.Uint32()) * lc.duration)
}
