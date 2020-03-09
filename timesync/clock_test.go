package timesync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const d50milli = 50 * time.Millisecond

func TestClock_StartClock(t *testing.T) {
	tick := d50milli
	c := RealClock{}
	ts := NewClock(c, tick, c.Now(), log.NewDefault(t.Name()))
	tk := ts.Subscribe()
	then := time.Now()
	ts.StartNotifying()

	select {
	case <-tk:
		dur := time.Now().Sub(then)
		assert.True(t, tick <= dur)
	}
	ts.Close()
}

func TestClock_StartClock_BeforeEpoch(t *testing.T) {
	tick := d50milli
	tmr := RealClock{}

	waitTime := 2 * d50milli
	ts := NewClock(tmr, tick, tmr.Now().Add(2*d50milli), log.NewDefault(t.Name()))
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

func TestClock_TickFutureGenesis(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewClock(tmr, d50milli, tmr.Now().Add(2*d50milli), log.NewDefault(t.Name()))
	assert.Equal(t, types.LayerID(0), ticker.lastTickedLayer) // check assumption that we are on genesis = 0
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	x := <-sub
	assert.Equal(t, types.LayerID(1), x)
	x = <-sub
	assert.Equal(t, types.LayerID(2), x)
}

func TestClock_TickPastGenesis(t *testing.T) {
	tmr := &RealClock{}
	ticker := NewClock(tmr, 2*d50milli, tmr.Now().Add(-7*d50milli), log.NewDefault(t.Name()))
	expectedTimeToTick := d50milli // tickInterval is 100ms and the genesis tick (layer 1) was 350ms ago
	/*
		T-350 -> layer 1
		T-250 -> layer 2
		T-150 -> layer 3
		T-50  -> layer 4
		T+50  -> layer 5
	*/
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	start := time.Now()
	x := <-sub
	duration := time.Since(start)
	assert.Equal(t, types.LayerID(5), x)
	assert.True(t, duration >= expectedTimeToTick, "tick happened too soon (%v)", duration)
	assert.True(t, duration < expectedTimeToTick+d50milli, "tick happened more than 50ms too late (%v)", duration)
}

func TestClock_NewClock(t *testing.T) {
	r := require.New(t)
	tmr := &RealClock{}
	ticker := NewClock(tmr, 100*time.Millisecond, tmr.Now().Add(-190*time.Millisecond), log.NewDefault(t.Name()))
	r.Equal(types.LayerID(2), ticker.lastTickedLayer)
}

func TestClock_CloseTwice(t *testing.T) {
	ld := d50milli
	clock := NewClock(RealClock{}, ld, time.Now(), log.NewDefault(t.Name()))
	clock.StartNotifying()
	clock.Close()
	clock.Close()
}
