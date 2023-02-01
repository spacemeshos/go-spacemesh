package timesync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func Test_NodeClock_GenesisTime(t *testing.T) {
	genesis := time.Now()

	clock := NewClock(RealClock{}, time.Second, genesis, logtest.New(t))
	require.NotNil(t, clock)

	require.Equal(t, genesis.Local(), clock.GenesisTime())
}

func Test_NodeClock_Close(t *testing.T) {
	clock := NewClock(RealClock{}, time.Second, time.Now(), logtest.New(t))
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
	clock := NewClock(RealClock{}, time.Second, time.Now(), logtest.New(t))
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
