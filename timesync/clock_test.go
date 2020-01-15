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

func TestClock_StartClock(t *testing.T) {
	tick := 1 * time.Second
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
	tick := 1 * time.Second
	tmr := RealClock{}

	waitTime := 2 * time.Second
	ts := NewClock(tmr, tick, tmr.Now().Add(2*time.Second), log.NewDefault(t.Name()))
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
	ticker := NewClock(tmr, 1*time.Second, tmr.Now().Add(2*time.Second), log.NewDefault(t.Name()))
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
	ticker := NewClock(tmr, 1*time.Second, tmr.Now().Add(-3900*time.Millisecond), log.NewDefault(t.Name()))
	sub := ticker.Subscribe()
	ticker.StartNotifying()
	start := time.Now()
	x := <-sub
	duration := time.Since(start)
	assert.Equal(t, types.LayerID(5), x)
	assert.True(t, duration > 99*time.Millisecond && duration < 107*time.Millisecond, duration)
}

func TestClock_NewClock(t *testing.T) {
	r := require.New(t)
	tmr := &RealClock{}
	ticker := NewClock(tmr, 100*time.Millisecond, tmr.Now().Add(-190*time.Millisecond), log.NewDefault(t.Name()))
	r.Equal(types.LayerID(2), ticker.lastTickedLayer)
}

func TestClock_CloseTwice(t *testing.T) {
	ld := time.Duration(20) * time.Second
	clock := NewClock(RealClock{}, ld, time.Now(), log.NewDefault(t.Name()))
	clock.StartNotifying()
	clock.Close()
	clock.Close()
}
