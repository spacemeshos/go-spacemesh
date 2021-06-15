package hare

import (
	"sync"
	"time"
)

const Wakeup = -2

type RoundClockWithCache struct {
	LayerTime     time.Time
	WakeupDelta   time.Duration
	RoundDuration time.Duration
	cache         map[int32]chan struct{}
	m             sync.RWMutex
}

func NewRoundClockWithCache(layerTime time.Time, wakeupDelta time.Duration, roundDuration time.Duration) *RoundClockWithCache {
	return &RoundClockWithCache{
		LayerTime:     layerTime,
		WakeupDelta:   wakeupDelta,
		RoundDuration: roundDuration,
		cache:         make(map[int32]chan struct{}),
	}
}

func (c *RoundClockWithCache) AwaitWakeup() <-chan struct{} {
	return c.AwaitEndOfRound(Wakeup)
}

func (c *RoundClockWithCache) AwaitEndOfRound(round int32) <-chan struct{} {
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

type SimpleRoundClock struct {
	LayerTime     time.Time
	WakeupDelta   time.Duration
	RoundDuration time.Duration
}

func NewSimpleRoundClock(layerTime time.Time, wakeupDelta time.Duration, roundDuration time.Duration) *SimpleRoundClock {
	return &SimpleRoundClock{
		LayerTime:     layerTime,
		WakeupDelta:   wakeupDelta,
		RoundDuration: roundDuration,
	}
}

func (c *SimpleRoundClock) AwaitWakeup() <-chan struct{} {
	return c.AwaitEndOfRound(Wakeup)
}

func (c *SimpleRoundClock) AwaitEndOfRound(round int32) <-chan struct{} {
	ch := make(chan struct{})
	// The pre-round (which has a value of -1) ends one RoundDuration after the WakeupDelta. Rounds, in general, are
	// zero-based. By adding 2 to the round number and multiplying by the RoundDuration we get the correct number of
	// RoundDurations to wait.
	duration := c.WakeupDelta + (c.RoundDuration * time.Duration(round+2))
	time.AfterFunc(c.LayerTime.Add(duration).Sub(time.Now()), func() {
		close(ch)
	})

	return ch
}
