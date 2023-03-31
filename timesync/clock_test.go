package timesync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func Test_NodeClock_NewClock(t *testing.T) {
	clock, err := NewClock(
		WithGenesisTime(time.Now()),
		WithTickInterval(time.Second),
		WithLogger(logtest.New(t)),
	)
	require.ErrorContains(t, err, "layer duration is zero")
	require.Nil(t, clock)

	clock, err = NewClock(
		WithLayerDuration(time.Second),
		WithTickInterval(time.Second),
		WithLogger(logtest.New(t)),
	)
	require.ErrorContains(t, err, "genesis time is zero")
	require.Nil(t, clock)

	clock, err = NewClock(
		WithLayerDuration(time.Second),
		WithTickInterval(time.Second),
		WithGenesisTime(time.Now()),
	)
	require.ErrorContains(t, err, "logger is nil")
	require.Nil(t, clock)

	clock, err = NewClock(
		WithLayerDuration(time.Second),
		WithGenesisTime(time.Now()),
		WithLogger(logtest.New(t)),
	)
	require.ErrorContains(t, err, "tick interval is zero")
	require.Nil(t, clock)

	clock, err = NewClock(
		WithLayerDuration(time.Second),
		WithTickInterval(2*time.Second),
		WithGenesisTime(time.Now()),
		WithLogger(logtest.New(t)),
	)
	require.ErrorContains(t, err, "tick interval must be between 0 and layer duration")
	require.Nil(t, clock)
}

func Test_NodeClock_GenesisTime(t *testing.T) {
	genesis := time.Now()

	clock, err := NewClock(
		WithLayerDuration(time.Second),
		WithTickInterval(time.Second/10),
		WithGenesisTime(genesis),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	require.Equal(t, genesis.Local(), clock.GenesisTime())
}

func Test_NodeClock_Close(t *testing.T) {
	clock, err := NewClock(
		WithLayerDuration(time.Second),
		WithTickInterval(time.Second/10),
		WithGenesisTime(time.Now()),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	var eg errgroup.Group
	eg.Go(func() error {
		if !assert.NotPanics(t, func() { clock.Close() }) {
			return fmt.Errorf("panic on first close")
		}
		return nil
	})
	eg.Go(func() error {
		if !assert.NotPanics(t, func() { clock.Close() }) {
			return fmt.Errorf("panic on second close")
		}
		return nil
	})

	require.NoError(t, eg.Wait())
}

func Test_NodeClock_NoRaceOnTick(t *testing.T) {
	clock, err := NewClock(
		WithLayerDuration(time.Second),
		WithTickInterval(time.Second/10),
		WithGenesisTime(time.Now()),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	ctx, cancel := context.WithCancel(context.Background())
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		for {
			select {
			case <-egCtx.Done():
				return egCtx.Err()
			case <-time.After(10 * time.Millisecond):
				clock.tick()
			}
		}
	})

	eg.Go(func() error {
		for {
			select {
			case <-egCtx.Done():
				return egCtx.Err()
			case <-time.After(11 * time.Millisecond):
				clock.tick()
			}
		}
	})

	<-time.After(1 * time.Second)
	cancel()
	require.ErrorIs(t, eg.Wait(), context.Canceled)
}

func Test_NodeClock_Await_BeforeGenesis(t *testing.T) {
	genesis := time.Now().Add(100 * time.Millisecond)
	layerDuration := 100 * time.Millisecond
	tickInterval := 10 * time.Millisecond

	clock, err := NewClock(
		WithLayerDuration(layerDuration),
		WithTickInterval(tickInterval),
		WithGenesisTime(genesis),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	select {
	case <-clock.AwaitLayer(types.NewLayerID(0)):
		require.WithinRange(t, time.Now(), genesis, genesis.Add(tickInterval))
	case <-time.After(1 * time.Second):
		require.Fail(t, "timeout")
	}
}

func Test_NodeClock_Await_PassedLayer(t *testing.T) {
	genesis := time.Now().Add(-1 * time.Second)
	layerDuration := 100 * time.Millisecond
	tickInterval := 10 * time.Millisecond

	clock, err := NewClock(
		WithLayerDuration(layerDuration),
		WithTickInterval(tickInterval),
		WithGenesisTime(genesis),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	select {
	case <-clock.AwaitLayer(types.NewLayerID(4)):
		require.Greater(t, time.Now(), genesis.Add(4*layerDuration))
	default:
		require.Fail(t, "await layer closed early")
	}
}

func Test_NodeClock_Await_WithClockMovingBackwards(t *testing.T) {
	now := time.Now()
	genesis := now.Add(-1 * time.Second)
	layerDuration := 100 * time.Millisecond
	tickInterval := 10 * time.Millisecond

	mClock := clock.NewMock()
	mClock.Set(now)

	clock, err := NewClock(
		withClock(mClock),
		WithLayerDuration(layerDuration),
		WithTickInterval(tickInterval),
		WithGenesisTime(genesis),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	// make the clock tick
	clock.tick()

	select {
	case <-clock.AwaitLayer(types.NewLayerID(10)):
		require.Greater(t, time.Now(), genesis.Add(10*layerDuration))
	default:
		require.Fail(t, "awaited layer didn't signal")
	}

	// move the clock backwards by a layer
	mClock.Set(now.Add(-2 * layerDuration))
	ch := clock.AwaitLayer(types.NewLayerID(9))

	select {
	case <-ch:
		require.Fail(t, "awaited layer shouldn't have signaled")
	default:
	}

	// continue clock forward in time
	mClock.Add(3 * layerDuration)

	select {
	case <-ch:
		require.Greater(t, time.Now(), genesis.Add(9*layerDuration))
	default:
		require.Fail(t, "awaited layer didn't signal")
	}
}

func Test_NodeClock_NonMonotonicTick_Forward(t *testing.T) {
	genesis := time.Now()
	layerDuration := 100 * time.Millisecond
	tickInterval := 10 * time.Millisecond

	mClock := clock.NewMock()
	mClock.Set(genesis.Add(5 * layerDuration))

	clock, err := NewClock(
		withClock(mClock),
		WithLayerDuration(layerDuration),
		WithTickInterval(tickInterval),
		WithGenesisTime(genesis),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)
	ch := clock.AwaitLayer(types.NewLayerID(6))

	select {
	case <-ch:
		require.Fail(t, "await layer closed early")
	case <-time.After(10 * time.Millisecond):
		// channel returned by AwaitLayer should not be closed yet and we should still be in layer 5
		require.Equal(t, types.NewLayerID(5), clock.CurrentLayer())
	}

	// hibernate for 5 layers (clock jumps forward)
	mClock.Add(5 * layerDuration)

	select {
	case <-ch:
		// channel returned by AwaitLayer should be closed and we should be in the future layer 10
		require.Equal(t, types.NewLayerID(10), clock.CurrentLayer())
	case <-time.After(10 * time.Millisecond):
		require.Fail(t, "await layer not closed")
	}
}

func Test_NodeClock_NonMonotonicTick_Backward(t *testing.T) {
	genesis := time.Now()
	layerDuration := 10 * time.Millisecond
	tickInterval := 1 * time.Millisecond

	mClock := clock.NewMock()
	mClock.Set(genesis.Add(5 * layerDuration))

	clock, err := NewClock(
		withClock(mClock),
		WithLayerDuration(layerDuration),
		WithTickInterval(tickInterval),
		WithGenesisTime(genesis),
		WithLogger(logtest.New(t)),
	)
	require.NoError(t, err)
	require.NotNil(t, clock)

	ch6 := clock.AwaitLayer(types.NewLayerID(6))
	ch7 := clock.AwaitLayer(types.NewLayerID(7))

	select {
	case <-ch6:
		require.Fail(t, "await layer 6 closed early")
	case <-ch7:
		require.Fail(t, "await layer 7 closed early")
	case <-time.After(10 * time.Millisecond):
		// channel returned by AwaitLayer should not be closed yet and we should still be in layer 5
		require.Equal(t, types.NewLayerID(5), clock.CurrentLayer())
	}

	// simulate time passing
	mClock.Add(1 * layerDuration)

	select {
	case <-ch6:
		// channel returned by AwaitLayer should be closed and we should be in layer 6
		require.Equal(t, types.NewLayerID(6), clock.CurrentLayer())
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "await layer 6 not closed")
	}

	select {
	case <-ch7:
		require.Fail(t, "await layer 7 closed early")
	default:
	}

	// NTP corrects time backwards
	mClock.Add(-1 * layerDuration)
	ch6 = clock.AwaitLayer(types.NewLayerID(6))

	select {
	case <-ch6:
		require.Fail(t, "await layer 6 closed early")
	case <-ch7:
		require.Fail(t, "await layer 7 closed early")
	case <-time.After(10 * time.Millisecond):
		// channel returned by AwaitLayer should not be closed yet and we should still be in layer 5
		require.Equal(t, types.NewLayerID(5), clock.CurrentLayer())
	}

	// simulate time passing
	mClock.Add(2 * layerDuration)

	select {
	case <-ch6:
		// channel returned by AwaitLayer should be closed
	case <-time.After(10 * time.Millisecond):
		require.Fail(t, "await layer 6 not closed")
	}

	select {
	case <-ch7:
		// channel returned by AwaitLayer should be closed and we should be in layer 7
		require.Equal(t, types.NewLayerID(7), clock.CurrentLayer())
	case <-time.After(10 * time.Millisecond):
		require.Fail(t, "await layer 7 not closed")
	}
}

func Fuzz_NodeClock_CurrentLayer(f *testing.F) {
	genesis, err := time.Parse(time.RFC3339, "2022-02-01T00:00:00Z")
	require.NoError(f, err)

	now, err := time.Parse(time.RFC3339, "2022-02-01T00:01:05Z")
	require.NoError(f, err)

	f.Add(uint32(genesis.Unix()), uint32(now.Unix()), uint32(time.Minute.Seconds()))
	f.Add(uint32(now.Unix()), uint32(genesis.Unix()), uint32(time.Minute.Seconds()))
	f.Add(uint32(genesis.Unix()), uint32(now.Add(10*time.Hour).Unix()), uint32(time.Hour.Seconds()))
	f.Fuzz(func(t *testing.T, genesis, now, layerSecs uint32) {
		if layerSecs == 0 {
			return
		}

		genesisTime := time.Unix(int64(genesis), 0)
		nowTime := time.Unix(int64(now), 0)

		layerTime := time.Duration(layerSecs) * time.Second
		tickInterval := layerTime / 10

		mClock := clock.NewMock()
		mClock.Set(nowTime)

		clock, err := NewClock(
			withClock(mClock),
			WithLayerDuration(layerTime),
			WithTickInterval(tickInterval),
			WithGenesisTime(genesisTime),
			WithLogger(logtest.New(t)),
		)
		require.NoError(t, err)
		require.NotNil(t, clock)

		expectedLayer := uint32(0)
		if nowTime.After(genesisTime) {
			expectedLayer = (now - genesis) / layerSecs
		}
		layer := clock.CurrentLayer()
		require.Equal(t, types.NewLayerID(expectedLayer), layer)
	})
}
