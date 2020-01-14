package timesync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func getTime() time.Time {
	layout := "2006-01-02T15:04:05.000Z"
	curr := "2018-11-12T11:45:26.000Z"
	tm, _ := time.Parse(layout, curr)

	return tm
}

func TestLayerConv_LayerToTime(t *testing.T) {
	r := require.New(t)
	tm := getTime()
	lc := LayerConv{5 * time.Second, tm}
	r.Equal(tm.Add(10*time.Second), lc.LayerToTime(3))
	r.Equal(lc.genesis, lc.LayerToTime(0))
}

func TestLayerConv_TimeToLayer(t *testing.T) {
	r := require.New(t)
	tm := getTime()
	lc := &LayerConv{5 * time.Second, tm}
	r.Equal(types.LayerID(2), lc.TimeToLayer(tm.Add(9*time.Second)))
	r.Equal(types.LayerID(3), lc.TimeToLayer(tm.Add(10*time.Second)))
	r.Equal(types.LayerID(3), lc.TimeToLayer(tm.Add(12*time.Second)))

	lc.genesis = tm.Add(2 * time.Second)
	r.Equal(types.LayerID(0), lc.TimeToLayer(tm))
}

func TestTicker_pingPong(t *testing.T) {
	r := require.New(t)
	tm := getTime()
	lc := LayerConv{5 * time.Second, tm}
	ttl := lc.TimeToLayer(tm.Add(9 * time.Second))
	r.Equal(types.LayerID(2), lc.TimeToLayer(lc.LayerToTime(ttl)))
}
