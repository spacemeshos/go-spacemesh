package hare

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClock_AwaitRound(t *testing.T) {
	r := require.New(t)

	tolerance := 20 * time.Millisecond
	waitTime := 80 * time.Millisecond

	wakeupDelta := 10 * time.Second
	roundDuration := 30 * time.Second
	layerTime := time.Now().
		Add(-(wakeupDelta + (5 * roundDuration))).
		Add(waitTime)

	c := NewSimpleRoundClock(layerTime, wakeupDelta, roundDuration)

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
