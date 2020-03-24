package timesync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"time"
)

// LayerConv is a converter between time to layer ID struct
type LayerConv struct {
	duration time.Duration // the layer duration, assumed to be > 0
	genesis  time.Time     // the genesis time
}

// TimeToLayer returns the layer of the provided time
func (lc LayerConv) TimeToLayer(t time.Time) types.LayerID {
	if t.Before(lc.genesis) { // the genesis is in the future
		return 0
	}
	return types.LayerID((t.Sub(lc.genesis) / lc.duration) + 1)
}

// LayerToTime returns the time of the provided layer
func (lc LayerConv) LayerToTime(id types.LayerID) time.Time {
	if id == 0 { // layer 1 is genesis, consider 0 also as genesis
		return lc.genesis
	}
	return lc.genesis.Add(time.Duration(id-1) * lc.duration)
}
