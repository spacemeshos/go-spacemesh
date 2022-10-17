package hare

import (
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClock_AwaitWakeup(t *testing.T) {
	c := NewSimpleRoundClock(time.Now(), 10*time.Millisecond, 8*time.Millisecond)

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
	if util.IsWindows() && util.IsCi() {
		t.Skip("Skipping test in Windows on CI (https://github.com/spacemeshos/go-spacemesh/issues/3628)")
	}

	r := require.New(t)

	tolerance := 5 * time.Millisecond
	waitTime := 20 * time.Millisecond

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
