package timesync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	ts := NewTicker(MockTimer{}, tick, start)
	tk := ts.Subscribe()
	then := time.Now()
	ts.StartNotifying()

	select {
	case <-tk:
		dur := time.Now().Sub(then)
		assert.True(t, tick < dur)
	}
	ts.Close()
}

func TestTicker_StartClock_BeforeEpoch(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:28.371Z"
	tmr := MockTimer{}
	start, _ := time.Parse(layout, str)

	waitTime := start.Sub(tmr.Now())
	ts := NewTicker(tmr, tick, start)
	tk := ts.Subscribe()
	then := time.Now()
	ts.StartNotifying()

	fmt.Println(waitTime)
	select {
	case <-tk:
		dur := time.Now().Sub(then)
		fmt.Println(dur)
		assert.True(t, waitTime < dur)
	}
	ts.Close()
}

func TestTicker_StartClock_LayerID(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:20.371Z"
	start, _ := time.Parse(layout, str)

	ts := NewTicker(MockTimer{}, tick, start)
	ts.updateLayerID()
	assert.Equal(t, types.LayerID(8), ts.nextLayerToTick)
	ts.Close()
}

func TestTicker_StartClock_2(t *testing.T) {
	destTime := 2 * time.Second
	tmr := &RealClock{}
	then := tmr.Now()
	ticker := NewTicker(tmr, 5*time.Second, then.Add(destTime))
	ticker.StartNotifying()
	sub := ticker.Subscribe()
	<-sub
	assert.True(t, tmr.Now().Sub(then).Seconds() <= float64(2.1))
}

func TestTicker_Tick(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewTicker(tmr, 5*time.Second, tmr.Now())
	ticker.started = true
	ticker.notifyOnTick()
	l := ticker.nextLayerToTick
	ticker.notifyOnTick()
	assert.Equal(t, ticker.nextLayerToTick, l+1)
}

func TestTicker_TickFutureGenesis(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewTicker(tmr, 1*time.Second, tmr.Now().Add(2*time.Second))
	assert.Equal(t, types.LayerID(1), ticker.nextLayerToTick) // check assumption that nextLayerToTick >= 1
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	x := <-sub
	assert.Equal(t, types.LayerID(1), x)
	x = <-sub
	assert.Equal(t, types.LayerID(2), x)
}

func TestTicker_TickPastGenesis(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewTicker(tmr, 1*time.Second, tmr.Now().Add(-3900*time.Millisecond))
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	start := time.Now()
	x := <-sub
	duration := time.Since(start)
	assert.Equal(t, types.LayerID(5), x)
	assert.True(t, duration > 99*time.Millisecond && duration < 105*time.Millisecond, duration)
}

func TestTicker_NewTicker(t *testing.T) {
	r := require.New(t)
	tmr := &RealClock{}
	ticker := NewTicker(tmr, 100*time.Millisecond, tmr.Now().Add(-190*time.Millisecond))
	r.False(ticker.started) // not started until call to StartNotifying
	r.Equal(types.LayerID(3), ticker.nextLayerToTick)
}
