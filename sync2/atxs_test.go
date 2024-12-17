package sync2_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sync2"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync/mocks"
	"github.com/spacemeshos/go-spacemesh/system"
)

func atxSeqResult(atxs []types.ATXID) rangesync.SeqResult {
	return rangesync.SeqResult{
		Seq: func(yield func(k rangesync.KeyBytes) bool) {
			for _, atx := range atxs {
				if !yield(atx.Bytes()) {
					return
				}
			}
		},
		Error: rangesync.NoSeqError,
	}
}

func TestAtxHandler_Success(t *testing.T) {
	const (
		batchSize       = 4
		maxAttempts     = 3
		maxBatchRetries = 2
		batchRetryDelay = 10 * time.Second
	)
	ctrl := gomock.NewController(t)
	allAtxs := make([]types.ATXID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allAtxs {
		allAtxs[i] = types.RandomATXID()
	}
	f := NewMockFetcher(ctrl)
	clock := clockwork.NewFakeClock()
	h := sync2.NewATXHandler(logger, f, batchSize, maxAttempts, maxBatchRetries, batchRetryDelay, clock)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allAtxs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id.Bytes()))
		f.EXPECT().RegisterPeerHashes(peer, []types.Hash32{id.Hash32()})
	}
	toFetch := make(map[types.ATXID]bool)
	for _, id := range allAtxs {
		toFetch[id] = true
	}
	var batches []int
	f.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, atxs []types.ATXID, opts ...system.GetAtxOpt) error {
			batches = append(batches, len(atxs))
			var atxOpts system.GetAtxOpts
			for _, opt := range opts {
				opt(&atxOpts)
			}
			require.NotNil(t, atxOpts.Callback)
			for _, id := range atxs {
				require.True(t, toFetch[id], "already fetched or bad ID")
				delete(toFetch, id)
				atxOpts.Callback(id, nil)
			}
			return nil
		}).Times(3)
	require.NoError(t, h.Commit(context.Background(), peer, baseSet, atxSeqResult(allAtxs)))
	require.Empty(t, toFetch)
	require.Equal(t, []int{4, 4, 2}, batches)
}

func TestAtxHandler_Retry(t *testing.T) {
	const (
		batchSize       = 4
		maxAttempts     = 3
		maxBatchRetries = 2
		batchRetryDelay = 10 * time.Second
	)
	ctrl := gomock.NewController(t)
	allAtxs := make([]types.ATXID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allAtxs {
		allAtxs[i] = types.RandomATXID()
	}
	f := NewMockFetcher(ctrl)
	clock := clockwork.NewFakeClock()
	h := sync2.NewATXHandler(logger, f, batchSize, maxAttempts, maxBatchRetries, batchRetryDelay, clock)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allAtxs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id.Bytes()))
		f.EXPECT().RegisterPeerHashes(peer, []types.Hash32{id.Hash32()})
	}
	failCount := 0
	var fetched []types.ATXID
	validationFailed := false
	f.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, atxs []types.ATXID, opts ...system.GetAtxOpt) error {
			errs := make(map[types.Hash32]error)
			var atxOpts system.GetAtxOpts
			for _, opt := range opts {
				opt(&atxOpts)
			}
			require.NotNil(t, atxOpts.Callback)
			for _, id := range atxs {
				switch {
				case id == allAtxs[0]:
					require.False(t, validationFailed, "retried after validation error")
					errs[id.Hash32()] = pubsub.ErrValidationReject
					atxOpts.Callback(id, errs[id.Hash32()])
					validationFailed = true
				case id == allAtxs[1] && failCount < 2:
					errs[id.Hash32()] = errors.New("fetch failed")
					atxOpts.Callback(id, errs[id.Hash32()])
					failCount++
				default:
					fetched = append(fetched, id)
					atxOpts.Callback(id, nil)
				}
			}
			if len(errs) > 0 {
				var bErr fetch.BatchError
				for h, err := range errs {
					bErr.Add(h, err)
				}
				return &bErr
			}
			return nil
		}).AnyTimes()

	// If it so happens that a full batch fails, we need to advance the clock to
	// trigger the retry.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	eg.Go(func() error {
		for {
			// FIXME: BlockUntilContext is not included in FakeClock interface.
			// This will be fixed in a post-0.4.0 clockwork release, but with a breaking change that
			// makes FakeClock a struct instead of an interface.
			// See: https://github.com/jonboulle/clockwork/pull/71
			clock.(interface {
				BlockUntilContext(ctx context.Context, n int) error
			}).BlockUntilContext(ctx, 1)
			if ctx.Err() != nil {
				return nil
			}
			clock.Advance(batchRetryDelay)
		}
	})

	require.NoError(t, h.Commit(context.Background(), peer, baseSet, atxSeqResult(allAtxs)))
	require.ElementsMatch(t, allAtxs[1:], fetched)
	cancel()
	require.NoError(t, eg.Wait())
}

func TestAtxHandler_Cancel(t *testing.T) {
	const (
		batchSize       = 4
		maxAttempts     = 3
		maxBatchRetries = 2
		batchRetryDelay = 10 * time.Second
	)
	atxID := types.RandomATXID()
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	f := NewMockFetcher(ctrl)
	clock := clockwork.NewFakeClock()
	h := sync2.NewATXHandler(logger, f, batchSize, maxAttempts, maxBatchRetries, batchRetryDelay, clock)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	baseSet.EXPECT().Has(rangesync.KeyBytes(atxID.Bytes())).Return(false, nil)
	f.EXPECT().RegisterPeerHashes(peer, []types.Hash32{atxID.Hash32()})
	f.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, atxs []types.ATXID, opts ...system.GetAtxOpt) error {
			return context.Canceled
		})
	sr := rangesync.SeqResult{
		Seq: func(yield func(k rangesync.KeyBytes) bool) {
			yield(atxID.Bytes())
		},
		Error: rangesync.NoSeqError,
	}
	require.ErrorIs(t, h.Commit(context.Background(), peer, baseSet, sr), context.Canceled)
}

func TestAtxHandler_BatchRetry(t *testing.T) {
	const (
		batchSize       = 4
		maxAttempts     = 3
		maxBatchRetries = 2
		batchRetryDelay = 10 * time.Second
	)
	ctrl := gomock.NewController(t)
	allAtxs := make([]types.ATXID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allAtxs {
		allAtxs[i] = types.RandomATXID()
	}
	clock := clockwork.NewFakeClock()
	f := NewMockFetcher(ctrl)
	h := sync2.NewATXHandler(logger, f, batchSize, maxAttempts, maxBatchRetries, batchRetryDelay, clock)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allAtxs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id.Bytes()))
		f.EXPECT().RegisterPeerHashes(peer, []types.Hash32{id.Hash32()})
	}
	f.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, atxs []types.ATXID, opts ...system.GetAtxOpt) error {
			return errors.New("fetch failed")
		})
	var eg errgroup.Group
	eg.Go(func() error {
		return h.Commit(context.Background(), peer, baseSet, atxSeqResult(allAtxs))
	})
	// wait for delay after 1st batch failure
	clock.BlockUntil(1)
	toFetch := make(map[types.ATXID]bool)
	for _, id := range allAtxs {
		toFetch[id] = true
	}
	f.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, atxs []types.ATXID, opts ...system.GetAtxOpt) error {
			var atxOpts system.GetAtxOpts
			for _, opt := range opts {
				opt(&atxOpts)
			}
			require.NotNil(t, atxOpts.Callback)
			for _, id := range atxs {
				require.True(t, toFetch[id], "already fetched or bad ID")
				delete(toFetch, id)
				atxOpts.Callback(id, nil)
			}
			return nil
		}).Times(3)
	clock.Advance(batchRetryDelay)
	require.NoError(t, eg.Wait())
	require.Empty(t, toFetch)
}

func TestAtxHandler_BatchRetry_Fail(t *testing.T) {
	const (
		batchSize       = 4
		maxAttempts     = 3
		maxBatchRetries = 2
		batchRetryDelay = 10 * time.Second
	)
	ctrl := gomock.NewController(t)
	allAtxs := make([]types.ATXID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allAtxs {
		allAtxs[i] = types.RandomATXID()
	}
	clock := clockwork.NewFakeClock()
	f := NewMockFetcher(ctrl)
	h := sync2.NewATXHandler(logger, f, batchSize, maxAttempts, maxBatchRetries, batchRetryDelay, clock)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allAtxs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id.Bytes()))
		f.EXPECT().RegisterPeerHashes(peer, []types.Hash32{id.Hash32()})
	}
	sr := rangesync.SeqResult{
		Seq: func(yield func(k rangesync.KeyBytes) bool) {
			for _, atx := range allAtxs {
				if !yield(atx.Bytes()) {
					return
				}
			}
		},
		Error: rangesync.NoSeqError,
	}
	f.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, atxs []types.ATXID, opts ...system.GetAtxOpt) error {
			return errors.New("fetch failed")
		}).Times(3)
	var eg errgroup.Group
	eg.Go(func() error {
		return h.Commit(context.Background(), peer, baseSet, sr)
	})
	for range 2 {
		clock.BlockUntil(1)
		clock.Advance(batchRetryDelay)
	}
	require.Error(t, eg.Wait())
}

func TestMultiEpochATXSyncer(t *testing.T) {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t)
	oldCfg := sync2.DefaultConfig()
	oldCfg.MaxDepth = 16
	newCfg := sync2.DefaultConfig()
	newCfg.MaxDepth = 24
	hss := NewMockHashSyncSource(ctrl)
	mhs := sync2.NewMultiEpochATXSyncer(logger, hss, oldCfg, newCfg, 1)
	ctx := context.Background()

	lastSynced, err := mhs.EnsureSync(ctx, 0, 0)
	require.NoError(t, err)
	require.Zero(t, lastSynced)

	var syncActions []string
	curIdx := 0
	hss.EXPECT().CreateHashSync(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(name string, cfg sync2.Config, epoch types.EpochID) sync2.HashSync {
			idx := curIdx
			curIdx++
			syncActions = append(syncActions,
				fmt.Sprintf("new %s epoch %d maxDepth %d -> %d", name, epoch, cfg.MaxDepth, idx))
			hs := NewMockHashSync(ctrl)
			hs.EXPECT().Load().DoAndReturn(func() error {
				syncActions = append(syncActions, fmt.Sprintf("load %d %s", idx, name))
				return nil
			}).AnyTimes()
			hs.EXPECT().StartAndSync(ctx).DoAndReturn(func(_ context.Context) error {
				syncActions = append(syncActions, fmt.Sprintf("start+sync %d %s", idx, name))
				return nil
			}).AnyTimes()
			hs.EXPECT().Start().DoAndReturn(func() {
				syncActions = append(syncActions, fmt.Sprintf("start %d %s", idx, name))
			}).AnyTimes()
			hs.EXPECT().Stop().DoAndReturn(func() {
				syncActions = append(syncActions, fmt.Sprintf("stop %d %s", idx, name))
			}).AnyTimes()
			return hs
		}).AnyTimes()

	// Last wait epoch 3, new epoch 3
	lastSynced, err = mhs.EnsureSync(ctx, 3, 3)
	require.NoError(t, err)
	require.Equal(t, []string{
		"new atx-sync-1 epoch 1 maxDepth 16 -> 0",
		"load 0 atx-sync-1",
		"new atx-sync-2 epoch 2 maxDepth 16 -> 1",
		"load 1 atx-sync-2",
		"new atx-sync-3 epoch 3 maxDepth 24 -> 2",
		"load 2 atx-sync-3",
		"start+sync 0 atx-sync-1",
		"start+sync 1 atx-sync-2",
		"start+sync 2 atx-sync-3",
	}, syncActions)
	syncActions = nil
	require.Equal(t, types.EpochID(3), lastSynced)

	// Advance to epoch 4 w/o wait
	lastSynced, err = mhs.EnsureSync(ctx, 0, 4)
	require.NoError(t, err)
	require.Equal(t, []string{
		"stop 2 atx-sync-3",
		"new atx-sync-3 epoch 3 maxDepth 16 -> 3",
		"load 3 atx-sync-3",
		"new atx-sync-4 epoch 4 maxDepth 24 -> 4",
		"load 4 atx-sync-4",
		"start 0 atx-sync-1",
		"start 1 atx-sync-2",
		"start 3 atx-sync-3",
		"start 4 atx-sync-4",
	}, syncActions)
	syncActions = nil
	require.Equal(t, types.EpochID(0), lastSynced)

	mhs.Stop()
	require.Equal(t, []string{
		"stop 0 atx-sync-1",
		"stop 1 atx-sync-2",
		"stop 3 atx-sync-3",
		"stop 4 atx-sync-4",
	}, syncActions)
}
