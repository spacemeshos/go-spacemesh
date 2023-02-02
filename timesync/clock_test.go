package timesync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func Test_NodeClock_GenesisTime(t *testing.T) {
	genesis := time.Now()

	clock := NewClock(clock.New(), time.Second, genesis, logtest.New(t))
	require.NotNil(t, clock)

	require.Equal(t, genesis.Local(), clock.GenesisTime())
}

func Test_NodeClock_Close(t *testing.T) {
	clock := NewClock(clock.New(), time.Second, time.Now(), logtest.New(t))
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
	clock := NewClock(clock.New(), time.Second, time.Now(), logtest.New(t))
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
	layerDuration := 10 * time.Millisecond

	clock := NewClock(clock.New(), layerDuration, genesis, logtest.New(t))
	require.NotNil(t, clock)

	select {
	case <-clock.AwaitLayer(types.LayerID{}):
		require.GreaterOrEqual(t, time.Now(), genesis)
	case <-time.After(1 * time.Second):
		require.Fail(t, "timeout")
	}
}

func Test_NodeClock_Await_PassedLayer(t *testing.T) {
	genesis := time.Now().Add(-100 * time.Millisecond)
	layerDuration := 10 * time.Millisecond

	clock := NewClock(clock.New(), layerDuration, genesis, logtest.New(t))
	require.NotNil(t, clock)

	select {
	case <-clock.AwaitLayer(types.NewLayerID(4)):
		require.GreaterOrEqual(t, time.Now(), genesis.Add(4*layerDuration))
	default:
		require.Fail(t, "await layer closed early")
	}
}

func Test_NodeClock_NonMonotonicTick_Forward(t *testing.T) {
	genesis := time.Now()
	layerDuration := 10 * time.Millisecond

	mClock := clock.NewMock()
	mClock.Set(genesis.Add(5 * layerDuration))
	clock := NewClock(mClock, layerDuration, genesis, logtest.New(t, zapcore.DebugLevel))
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
		require.Equal(t, types.NewLayerID(15), clock.CurrentLayer())
	case <-time.After(10 * time.Millisecond):
		require.Fail(t, "await layer not closed")
	}
}

func Test_NodeClock_NonMonotonicTick_Backward(t *testing.T) {
	genesis := time.Now()
	layerDuration := 10 * time.Millisecond

	mClock := clock.NewMock()
	mClock.Set(genesis.Add(5 * layerDuration))
	clock := NewClock(mClock, layerDuration, genesis, logtest.New(t, zapcore.DebugLevel))
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

		mClock := clock.NewMock()
		mClock.Set(nowTime)
		clock := NewClock(mClock, layerTime, genesisTime, logtest.New(t))
		require.NotNil(t, clock)

		expectedLayer := uint32(0)
		if nowTime.After(genesisTime) {
			expectedLayer = (now - genesis) / layerSecs
		}
		layer := clock.CurrentLayer()
		require.Equal(t, types.NewLayerID(expectedLayer), layer)
	})
}
