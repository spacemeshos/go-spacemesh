package timesync

import (
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type MockTimer struct {

}

func (MockTimer) Now() time.Time {
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	return start
}

func TestTicker_StartClock(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)

	ts := NewTicker(MockTimer{},tick, start)
	tk := ts.Subscribe()
	then := time.Now()
	ts.Start()

	select {
		case <-tk:
			dur := time.Now().Sub(then)
			assert.True(t, tick < dur)
	}
	ts.Stop()
}

func TestTicker_StartClock_BeforeEpoch(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:28.371Z"
	tmr := MockTimer{}
	start, _ := time.Parse(layout, str)

	waitTime := start.Sub(tmr.Now())
	ts := NewTicker(tmr,tick, start)
	tk := ts.Subscribe()
	then := time.Now()
	ts.Start()

	select {
	case <-tk:
		dur := time.Now().Sub(then)
		assert.True(t, waitTime < dur)
	}
	ts.Stop()
}

func TestTicker_StartClock_LayerID(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:20.371Z"
	start, _ := time.Parse(layout, str)

	ts := NewTicker(MockTimer{},tick, start)
	ts.updateLayerID()
	assert.Equal(t, mesh.LayerID(6), ts.currentLayer)
	ts.Stop()
}