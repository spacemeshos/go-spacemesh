package hare

import (
	"math"
	"sync"
	"time"
)

// Wakeup has a value of -2 because it occurs exactly two round durations before the end of round 0.
// This accounts for the duration of round zero and the preround.
const Wakeup = math.MaxUint32 - 1 // -2

// RoundClockWithCache is a Hare-round clock with a cache that allows repeatedly calling AwaitEndOfRound() and getting
// the same instance of a timer. The cache management and synchronization require some additional resources.
type RoundClockWithCache struct {
	LayerTime     time.Time
	WakeupDelta   time.Duration
	RoundDuration time.Duration
	cache         map[uint32]chan struct{}
	m             sync.RWMutex
}

// NewRoundClockWithCache returns a new RoundClockWithCache, given the provided configuration.
func NewRoundClockWithCache(layerTime time.Time, wakeupDelta, roundDuration time.Duration) *RoundClockWithCache {
	return &RoundClockWithCache{
		LayerTime:     layerTime,
		WakeupDelta:   wakeupDelta,
		RoundDuration: roundDuration,
		cache:         make(map[uint32]chan struct{}),
	}
}

// AwaitWakeup returns a channel that gets closed when the Hare wakeup delay has passed.
func (c *RoundClockWithCache) AwaitWakeup() <-chan struct{} {
	return c.AwaitEndOfRound(Wakeup)
}

// AwaitEndOfRound returns a channel that gets closed when the given round ends. Repeated calls to this method will
// return the same instance of the channel and use the same underlying timer.
func (c *RoundClockWithCache) AwaitEndOfRound(round uint32) <-chan struct{} {
	c.m.RLock()
	if ch, ok := c.cache[round]; ok {
		c.m.RUnlock()
		return ch
	}
	c.m.RUnlock()

	c.m.Lock()
	defer c.m.Unlock()
	if ch, ok := c.cache[round]; ok {
		return ch
	}
	ch := make(chan struct{})
	c.cache[round] = ch

	// The pre-round (which has a value of -1) ends one RoundDuration after the WakeupDelta. Rounds, in general, are
	// zero-based. By adding 2 to the round number and multiplying by the RoundDuration we get the correct number of
	// RoundDurations to wait.
	duration := c.WakeupDelta + (c.RoundDuration * time.Duration(round+2))
	time.AfterFunc(c.LayerTime.Add(duration).Sub(time.Now()), func() {
		c.m.Lock()
		defer c.m.Unlock()

		close(ch)
		delete(c.cache, round)
	})

	return ch
}

// SimpleRoundClock is a simple Hare-round clock. Repeated calls to AwaitEndOfRound() will each return a new channel
// and will be backed by a new timer which will not be garbage collected until it expires. To avoid leaking resources,
// callers should avoid calling AwaitEndOfRound() more than once for a given round (e.g. by storing the resulting
// channel and reusing it).
type SimpleRoundClock struct {
	LayerTime     time.Time
	WakeupDelta   time.Duration
	RoundDuration time.Duration
}

// NewSimpleRoundClock returns a new SimpleRoundClock, given the provided configuration.
func NewSimpleRoundClock(layerTime time.Time, wakeupDelta, roundDuration time.Duration) *SimpleRoundClock {
	return &SimpleRoundClock{
		LayerTime:     layerTime,
		WakeupDelta:   wakeupDelta,
		RoundDuration: roundDuration,
	}
}

// AwaitWakeup returns a channel that gets closed when the Hare wakeup delay has passed.
func (c *SimpleRoundClock) AwaitWakeup() <-chan struct{} {
	return c.AwaitEndOfRound(Wakeup)
}

// AwaitEndOfRound returns a channel that gets closed when the given round ends. Repeated calls to this method will
// return new instances of the channel and use new underlying timers which will not be garbage collected until they
// expire. To avoid leaking resources, callers should avoid calling AwaitEndOfRound() more than once for a given round
// (e.g. by storing the resulting channel and reusing it).
func (c *SimpleRoundClock) AwaitEndOfRound(round uint32) <-chan struct{} {
	ch := make(chan struct{})
	// The pre-round (which has a value of MaxUInt32) ends one RoundDuration after the WakeupDelta. Rounds, in general, are
	// zero-based. By adding 2 to the round number and multiplying by the RoundDuration we get the correct number of
	// RoundDurations to wait.
	duration := c.WakeupDelta + (c.RoundDuration * time.Duration(round+2))
	time.AfterFunc(c.LayerTime.Add(duration).Sub(time.Now()), func() {
		close(ch)
	})

	return ch
}
