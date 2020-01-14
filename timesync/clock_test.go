package timesync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
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
	c := RealClock{}
	ts := NewClock(c, tick, c.Now())
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
	tmr := RealClock{}

	waitTime := 2 * time.Second
	ts := NewClock(tmr, tick, tmr.Now().Add(2*time.Second))
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

func TestTicker_TickFutureGenesis(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewClock(tmr, 1*time.Second, tmr.Now().Add(2*time.Second))
	assert.Equal(t, types.LayerID(1), ticker.lastTickedLayer+1) // check assumption that nextLayerToTick >= 1
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	x := <-sub
	assert.Equal(t, types.LayerID(1), x)
	x = <-sub
	assert.Equal(t, types.LayerID(2), x)
}

func TestTicker_TickPastGenesis(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewClock(tmr, 1*time.Second, tmr.Now().Add(-3900*time.Millisecond))
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	start := time.Now()
	x := <-sub
	duration := time.Since(start)
	assert.Equal(t, types.LayerID(5), x)
	assert.True(t, duration > 99*time.Millisecond && duration < 107*time.Millisecond, duration)
}

func TestTicker_NewClock(t *testing.T) {
	r := require.New(t)
	tmr := &RealClock{}
	ticker := NewClock(tmr, 100*time.Millisecond, tmr.Now().Add(-190*time.Millisecond))
	r.Equal(types.LayerID(2), ticker.lastTickedLayer)
}

func TestTicker_CloseTwice(t *testing.T) {
	ld := time.Duration(20) * time.Second
	clock := NewClock(RealClock{}, ld, time.Now())
	clock.StartNotifying()
	clock.Close()
	clock.Close()
}

func TestTicker_AwaitLayer(t *testing.T) {
	r := require.New(t)

	tmr := &RealClock{}
	ticker := NewTicker(tmr, LayerConv{
		duration: 10 * time.Millisecond,
		genesis:  tmr.Now(),
	})

	l := ticker.GetCurrentLayer() + 1
	ch := ticker.AwaitLayer(l)

	select {
	case <-ch:
		r.Fail("got notified before layer ticked")
	default:
	}

	time.Sleep(10 * time.Millisecond)
	ticker.StartNotifying()
	missedTicks, err := ticker.Notify()
	r.NoError(err)
	r.Zero(missedTicks)

	select {
	case <-ch:
	default:
		r.Fail("did not get notified despite layer ticking")
	}

	ch2 := ticker.AwaitLayer(l)

	r.NotEqual(ch, ch2) // original channel should be discarded and a constant closedChannel should be returned
	select {
	case <-ch2:
	default:
		r.Fail("returned channel was not closed, despite awaiting past layer")
	}

	ch3 := ticker.AwaitLayer(l - 1)

	r.Equal(ch2, ch3) // the same closedChannel should be returned for all past layers
}
