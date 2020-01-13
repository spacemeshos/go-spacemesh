package timesync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"time"
)

type LayerConv struct {
	duration time.Duration // the layer duration
	genesis  time.Time     // the genesis time
}

func (lc LayerConv) TimeToLayer(t time.Time) types.LayerID {
	return types.LayerID((t.Sub(lc.genesis) / lc.duration * time.Second) + 2)
}

func (lc LayerConv) LayerToTime(id types.LayerID) time.Time {
	return lc.genesis.Add(time.Duration(id) * lc.duration)
}
