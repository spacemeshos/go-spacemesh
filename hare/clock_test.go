package hare

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestClock_AwaitWakeup(t *testing.T) {
	c := NewRoundClockWithCache(time.Now(), 10*time.Millisecond, 8*time.Millisecond)

	ch := c.AwaitWakeup()
	select {
	case <-ch:
		assert.Fail(t, "too fast")
	case <-time.After(8 * time.Millisecond):
	}

	select {
	case <-ch:
	case <-time.After(4 * time.Millisecond):
		assert.Fail(t, "too slow")
	}
}

func TestClock_AwaitRound(t *testing.T) {
	r := require.New(t)

	tolerance := 5 * time.Millisecond
	waitTime := 20 * time.Millisecond

	wakeupDelta := 10 * time.Second
	roundDuration := 30 * time.Second
	layerTime := time.Now().
		Add(-(wakeupDelta + (5 * roundDuration))).
		Add(waitTime)

	c := NewRoundClockWithCache(layerTime, wakeupDelta, roundDuration)

	ch := c.AwaitEndOfRound(3)

	select {
	case <-ch:
		r.Fail("too fast")
	case <-time.After(waitTime - tolerance):
		// Good
	}

	select {
	case <-ch:
		// Success
	case <-time.After(2 * tolerance):
		r.Fail("too slow")
	}
}
