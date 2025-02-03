package sync2_test

import (
	"context"
	"encoding/hex"
	"errors"
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
)

var testCfg = sync2.Config{
	BatchSize:        4,
	MaxAttempts:      3,
	MaxBatchRetries:  2,
	FailedBatchDelay: 10 * time.Second,
}

type fakeID [4]byte

func (id fakeID) ShortString() string {
	return hex.EncodeToString(id[:])
}

func (id fakeID) Bytes() []byte {
	return id[:]
}

func (id fakeID) Hash32() types.Hash32 {
	var h types.Hash32
	copy(h[:], id[:])
	return h
}

func randomFakeID() fakeID {
	var id fakeID
	copy(id[:], types.RandomBytes(len(id)))
	return id
}

func TestCommitState_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	allIDs := make([]fakeID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allIDs {
		allIDs[i] = randomFakeID()
	}
	clock := clockwork.NewFakeClock()
	h := NewMockHandler[fakeID](ctrl)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allIDs {
		baseSet.EXPECT().Has(id.Bytes())
		h.EXPECT().Register(peer, id.Bytes()).Return(id)
	}
	toFetch := make(map[fakeID]bool)
	for _, id := range allIDs {
		toFetch[id] = true
	}
	var batches []int
	h.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []fakeID, callback func(fakeID, error)) error {
			batches = append(batches, len(ids))
			for _, id := range ids {
				require.True(t, toFetch[id], "already fetched or bad ID")
				delete(toFetch, id)
				callback(id, nil)
			}
			return nil
		}).Times(3)
	cs, err := sync2.NewCommitState(logger, h, clock, peer, baseSet, byteSeqResult(allIDs), testCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Commit(context.Background()))
	require.Empty(t, toFetch)
	require.Equal(t, []int{4, 4, 2}, batches)
}

func TestCommitState_Retry(t *testing.T) {
	ctrl := gomock.NewController(t)
	allIDs := make([]fakeID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allIDs {
		allIDs[i] = randomFakeID()
	}
	clock := clockwork.NewFakeClock()
	h := NewMockHandler[fakeID](ctrl)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allIDs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id[:]))
		h.EXPECT().Register(peer, rangesync.KeyBytes(id[:])).Return(id)
	}
	toFetch := make(map[fakeID]bool)
	for _, id := range allIDs {
		toFetch[id] = true
	}
	failCount := 0
	var fetched []fakeID
	validationFailed := false
	h.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []fakeID, callback func(fakeID, error)) error {
			errs := make(map[types.Hash32]error)
			for _, id := range ids {
				switch {
				case id == allIDs[0]:
					require.False(t, validationFailed, "retried after validation error")
					errs[id.Hash32()] = pubsub.ErrValidationReject
					callback(id, errs[id.Hash32()])
					validationFailed = true
				case id == allIDs[1] && failCount < 2:
					errs[id.Hash32()] = errors.New("fetch failed")
					callback(id, errs[id.Hash32()])
					failCount++
				default:
					fetched = append(fetched, id)
					callback(id, nil)
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

	cs, err := sync2.NewCommitState(logger, h, clock, peer, baseSet, byteSeqResult(allIDs), testCfg)
	require.NoError(t, err)

	// If it so happens that a full batch fails, we need to advance the clock to
	// trigger the retry.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	eg.Go(func() error {
		for {
			clock.BlockUntilContext(ctx, 1)
			if ctx.Err() != nil {
				return nil
			}
			clock.Advance(testCfg.FailedBatchDelay)
		}
	})

	require.NoError(t, cs.Commit(context.Background()))
	require.ElementsMatch(t, allIDs[1:], fetched)
	cancel()
	require.NoError(t, eg.Wait())
}

func TestCommitState_Cancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	id := randomFakeID()
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	clock := clockwork.NewFakeClock()
	h := NewMockHandler[fakeID](ctrl)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	baseSet.EXPECT().Has(rangesync.KeyBytes(id[:]))
	h.EXPECT().Register(peer, rangesync.KeyBytes(id[:])).Return(id)
	h.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []fakeID, callback func(fakeID, error)) error {
			return context.Canceled
		})
	cs, err := sync2.NewCommitState(logger, h, clock, peer, baseSet, byteSeqResult([]fakeID{id}), testCfg)
	require.NoError(t, err)
	require.ErrorIs(t, cs.Commit(context.Background()), context.Canceled)
}

func TestCommitState_BatchRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	allIDs := make([]fakeID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allIDs {
		allIDs[i] = randomFakeID()
	}
	clock := clockwork.NewFakeClock()
	h := NewMockHandler[fakeID](ctrl)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allIDs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id[:]))
		h.EXPECT().Register(peer, rangesync.KeyBytes(id[:])).Return(id)
	}
	h.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []fakeID, callback func(fakeID, error)) error {
			return errors.New("fetch failed")
		})
	var eg errgroup.Group
	cs, err := sync2.NewCommitState(logger, h, clock, peer, baseSet, byteSeqResult(allIDs), testCfg)
	require.NoError(t, err)
	eg.Go(func() error {
		return cs.Commit(context.Background())
	})
	// wait for delay after 1st batch failure
	clock.BlockUntilContext(context.Background(), 1)
	toFetch := make(map[fakeID]bool)
	for _, id := range allIDs {
		toFetch[id] = true
	}
	h.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []fakeID, callback func(fakeID, error)) error {
			for _, id := range ids {
				require.True(t, toFetch[id], "already fetched or bad ID")
				delete(toFetch, id)
				callback(id, nil)
			}
			return nil
		}).Times(3)
	clock.Advance(testCfg.FailedBatchDelay)
	require.NoError(t, eg.Wait())
	require.Empty(t, toFetch)
}

func TestCommitState_BatchRetry_Fail(t *testing.T) {
	ctrl := gomock.NewController(t)
	allIDs := make([]fakeID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allIDs {
		allIDs[i] = randomFakeID()
	}
	clock := clockwork.NewFakeClock()
	h := NewMockHandler[fakeID](ctrl)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allIDs {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id[:]))
		h.EXPECT().Register(peer, rangesync.KeyBytes(id[:])).Return(id)
	}
	h.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []fakeID, callback func(fakeID, error)) error {
			return errors.New("fetch failed")
		}).Times(3)
	var eg errgroup.Group
	cs, err := sync2.NewCommitState(logger, h, clock, peer, baseSet, byteSeqResult(allIDs), testCfg)
	require.NoError(t, err)
	eg.Go(func() error {
		return cs.Commit(context.Background())
	})
	for range 2 {
		clock.BlockUntilContext(context.Background(), 1)
		clock.Advance(testCfg.FailedBatchDelay)
	}
	require.Error(t, eg.Wait())
}
