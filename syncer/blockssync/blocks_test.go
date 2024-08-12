package blockssync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestCanBeAggregated(t *testing.T) {
	var (
		ctrl        = gomock.NewController(t)
		fetch       = NewMockblockFetcher(ctrl)
		req         = make(chan []types.BlockID, 10)
		out         = make(chan []types.BlockID, 10)
		ctx, cancel = context.WithCancel(context.Background())
		eg          errgroup.Group
	)
	t.Cleanup(func() {
		cancel()
		eg.Wait()
	})
	eg.Go(func() error {
		return Sync(ctx, zaptest.NewLogger(t), req, fetch)
	})
	fetch.EXPECT().
		GetBlocks(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, blocks []types.BlockID) error {
			out <- blocks
			return nil
		}).
		AnyTimes()
	first := []types.BlockID{{1}, {2}, {3}}
	second := []types.BlockID{{2}, {3}, {4}}
	req <- first
	req <- second
	rst1 := timedRead(t, out)
	require.Subset(t, rst1, first)
	if len(rst1) == len(first) {
		require.Subset(t, timedRead(t, out), second)
	}
}

func TestErrorDoesntExit(t *testing.T) {
	var (
		ctrl        = gomock.NewController(t)
		fetch       = NewMockblockFetcher(ctrl)
		req         = make(chan []types.BlockID, 10)
		out         = make(chan []types.BlockID, 10)
		ctx, cancel = context.WithCancel(context.Background())
		eg          errgroup.Group
	)
	t.Cleanup(func() {
		cancel()
		eg.Wait()
	})
	eg.Go(func() error {
		return Sync(ctx, zaptest.NewLogger(t), req, fetch)
	})
	fetch.EXPECT().
		GetBlocks(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, blocks []types.BlockID) error {
			out <- blocks
			return errors.New("test")
		}).
		AnyTimes()
	first := []types.BlockID{{1}, {2}, {3}}
	req <- first
	require.Subset(t, timedRead(t, out), first)
	req <- first
	require.Subset(t, timedRead(t, out), first)
}

func timedRead(tb testing.TB, blocks chan []types.BlockID) []types.BlockID {
	delay := time.Second
	select {
	case v := <-blocks:
		return v
	case <-time.After(delay):
		require.FailNow(tb, "timed out after", delay)
	}
	return nil
}
