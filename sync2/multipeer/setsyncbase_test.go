package multipeer

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	rmocks "github.com/spacemeshos/go-spacemesh/sync2/rangesync/mocks"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type setSyncBaseTester struct {
	*testing.T
	ctrl    *gomock.Controller
	ps      *MockPairwiseSyncer
	os      *rmocks.MockOrderedSet
	ssb     *SetSyncBase
	waitMtx sync.Mutex
	waitChs map[string]chan error
	doneCh  chan types.Ordered
}

func newSetSyncBaseTester(t *testing.T, os rangesync.OrderedSet) *setSyncBaseTester {
	ctrl := gomock.NewController(t)
	st := &setSyncBaseTester{
		T:       t,
		ctrl:    ctrl,
		ps:      NewMockPairwiseSyncer(ctrl),
		waitChs: make(map[string]chan error),
		doneCh:  make(chan types.Ordered),
	}
	if os == nil {
		st.os = rmocks.NewMockOrderedSet(ctrl)
		os = st.os
	}
	st.ssb = NewSetSyncBase(st.ps, os, func(ctx context.Context, k types.Ordered, p p2p.Peer) error {
		err := <-st.getWaitCh(k.(types.KeyBytes))
		st.doneCh <- k
		return err
	})
	return st
}

func (st *setSyncBaseTester) getWaitCh(k types.KeyBytes) chan error {
	st.waitMtx.Lock()
	defer st.waitMtx.Unlock()
	ch, found := st.waitChs[string(k)]
	if !found {
		ch = make(chan error)
		st.waitChs[string(k)] = ch
	}
	return ch
}

func (st *setSyncBaseTester) expectCopy(ctx context.Context, addedKeys ...types.KeyBytes) *rmocks.MockOrderedSet {
	copy := rmocks.NewMockOrderedSet(st.ctrl)
	st.os.EXPECT().Copy().DoAndReturn(func() rangesync.OrderedSet {
		for _, k := range addedKeys {
			copy.EXPECT().Add(ctx, k)
		}
		return copy
	})
	return copy
}

func (st *setSyncBaseTester) expectSyncStore(
	ctx context.Context,
	p p2p.Peer,
	ss Syncer,
	addedKeys ...types.KeyBytes,
) {
	st.ps.EXPECT().SyncStore(ctx, p, ss, nil, nil).
		DoAndReturn(func(
			ctx context.Context,
			p p2p.Peer,
			os rangesync.OrderedSet,
			x, y types.KeyBytes,
		) error {
			for _, k := range addedKeys {
				require.NoError(st, os.Add(ctx, k))
			}
			return nil
		})
}

func (st *setSyncBaseTester) failToSyncStore(
	ctx context.Context,
	p p2p.Peer,
	ss Syncer,
	err error,
) {
	st.ps.EXPECT().SyncStore(ctx, p, ss, nil, nil).
		DoAndReturn(func(ctx context.Context, p p2p.Peer, os rangesync.OrderedSet, x, y types.KeyBytes) error {
			return err
		})
}

func (st *setSyncBaseTester) wait(count int) ([]types.KeyBytes, error) {
	var eg errgroup.Group
	eg.Go(st.ssb.Wait)
	var handledKeys []types.KeyBytes
	for k := range st.doneCh {
		handledKeys = append(handledKeys, k.(types.KeyBytes).Clone())
		count--
		if count == 0 {
			break
		}
	}
	return handledKeys, eg.Wait()
}

func TestSetSyncBase(t *testing.T) {
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()
		expPr := rangesync.ProbeResult{
			FP:    types.RandomFingerprint(),
			Count: 42,
			Sim:   0.99,
		}
		store := st.expectCopy(ctx)
		st.ps.EXPECT().Probe(ctx, p2p.Peer("p1"), store, nil, nil).Return(expPr, nil)
		pr, err := st.ssb.Probe(ctx, p2p.Peer("p1"))
		require.NoError(t, err)
		require.Equal(t, expPr, pr)
	})

	t.Run("single key one-time sync", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		addedKey := types.RandomKeyBytes(32)
		st.expectCopy(ctx, addedKey)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		x := types.RandomKeyBytes(32)
		y := types.RandomKeyBytes(32)
		st.ps.EXPECT().SyncStore(ctx, p2p.Peer("p1"), ss, x, y)
		require.NoError(t, ss.Sync(ctx, x, y))

		st.os.EXPECT().Has(gomock.Any(), addedKey)
		st.os.EXPECT().Add(ctx, addedKey)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, addedKey)
		require.NoError(t, ss.Sync(ctx, nil, nil))
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.KeyBytes{addedKey}, handledKeys)
	})

	t.Run("single key synced multiple times", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		addedKey := types.RandomKeyBytes(32)
		st.expectCopy(ctx, addedKey, addedKey, addedKey)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		// added just once
		st.os.EXPECT().Add(ctx, addedKey)
		for i := 0; i < 3; i++ {
			st.os.EXPECT().Has(gomock.Any(), addedKey)
			st.expectSyncStore(ctx, p2p.Peer("p1"), ss, addedKey)
			require.NoError(t, ss.Sync(ctx, nil, nil))
		}
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.KeyBytes{addedKey}, handledKeys)
	})

	t.Run("multiple keys", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		k1 := types.RandomKeyBytes(32)
		k2 := types.RandomKeyBytes(32)
		st.expectCopy(ctx, k1, k2)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		st.os.EXPECT().Has(gomock.Any(), k1)
		st.os.EXPECT().Has(gomock.Any(), k2)
		st.os.EXPECT().Add(ctx, k1)
		st.os.EXPECT().Add(ctx, k2)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, k1, k2)
		require.NoError(t, ss.Sync(ctx, nil, nil))
		close(st.getWaitCh(k1))
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.KeyBytes{k1, k2}, handledKeys)
	})

	t.Run("handler failure", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		k1 := types.RandomKeyBytes(32)
		k2 := types.RandomKeyBytes(32)
		st.expectCopy(ctx, k1, k2)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		st.os.EXPECT().Has(gomock.Any(), k1)
		st.os.EXPECT().Has(gomock.Any(), k2)
		// k1 is not propagated to syncBase due to the handler failure
		st.os.EXPECT().Add(ctx, k2)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, k1, k2)
		require.NoError(t, ss.Sync(ctx, nil, nil))
		handlerErr := errors.New("fail")
		st.getWaitCh(k1) <- handlerErr
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, []types.KeyBytes{k1, k2}, handledKeys)
	})

	t.Run("synctree based item store", func(t *testing.T) {
		t.Parallel()
		hs := make([]types.KeyBytes, 4)
		for n := range hs {
			hs[n] = types.RandomKeyBytes(32)
		}
		os := rangesync.NewDumbHashSet(true)
		os.Add(context.Background(), hs[0])
		os.Add(context.Background(), hs[1])
		st := newSetSyncBaseTester(t, os)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		ss.(rangesync.OrderedSet).Add(context.Background(), hs[2])
		ss.(rangesync.OrderedSet).Add(context.Background(), hs[3])
		// syncer's cloned ItemStore has new key immediately
		has, err := ss.(rangesync.OrderedSet).Has(context.Background(), hs[2])
		require.NoError(t, err)
		require.True(t, has)
		has, err = ss.(rangesync.OrderedSet).Has(context.Background(), hs[3])
		require.True(t, has)
		handlerErr := errors.New("fail")
		st.getWaitCh(hs[2]) <- handlerErr
		close(st.getWaitCh(hs[3]))
		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, hs[2:], handledKeys)
		// only successfully handled key propagate the syncBase
		has, err = os.Has(context.Background(), hs[2])
		require.False(t, has)
		has, err = os.Has(context.Background(), hs[3])
		require.True(t, has)
	})
}
