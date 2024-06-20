package hashsync

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"
)

type setSyncBaseTester struct {
	*testing.T
	ctrl    *gomock.Controller
	ps      *MockPairwiseSyncer
	is      *MockItemStore
	ssb     *SetSyncBase
	waitMtx sync.Mutex
	waitChs map[Ordered]chan error
	doneCh  chan Ordered
}

func newSetSyncBaseTester(t *testing.T, is ItemStore) *setSyncBaseTester {
	ctrl := gomock.NewController(t)
	st := &setSyncBaseTester{
		T:       t,
		ctrl:    ctrl,
		ps:      NewMockPairwiseSyncer(ctrl),
		waitChs: make(map[Ordered]chan error),
		doneCh:  make(chan Ordered),
	}
	if is == nil {
		st.is = NewMockItemStore(ctrl)
		is = st.is
	}
	st.ssb = NewSetSyncBase(st.ps, is, func(ctx context.Context, k Ordered, p p2p.Peer) error {
		err := <-st.getWaitCh(k)
		st.doneCh <- k
		return err
	})
	return st
}

func (st *setSyncBaseTester) getWaitCh(k Ordered) chan error {
	st.waitMtx.Lock()
	defer st.waitMtx.Unlock()
	ch, found := st.waitChs[k]
	if !found {
		ch = make(chan error)
		st.waitChs[k] = ch
	}
	return ch
}

func (st *setSyncBaseTester) expectCopy(ctx context.Context, addedKeys ...types.Hash32) *MockItemStore {
	copy := NewMockItemStore(st.ctrl)
	st.is.EXPECT().Copy().DoAndReturn(func() ItemStore {
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
	addedKeys ...types.Hash32,
) {
	st.ps.EXPECT().SyncStore(ctx, p, ss, nil, nil).
		DoAndReturn(func(ctx context.Context, p p2p.Peer, is ItemStore, x, y *types.Hash32) error {
			for _, k := range addedKeys {
				require.NoError(st, is.Add(ctx, k))
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
		DoAndReturn(func(ctx context.Context, p p2p.Peer, is ItemStore, x, y *types.Hash32) error {
			return err
		})
}

func (st *setSyncBaseTester) wait(count int) ([]types.Hash32, error) {
	var eg errgroup.Group
	eg.Go(st.ssb.Wait)
	var handledKeys []types.Hash32
	for k := range st.doneCh {
		handledKeys = append(handledKeys, k.(types.Hash32))
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
		expPr := ProbeResult{
			FP:    types.RandomHash(),
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

		addedKey := types.RandomHash()
		st.expectCopy(ctx, addedKey)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		x := types.RandomHash()
		y := types.RandomHash()
		st.ps.EXPECT().SyncStore(ctx, p2p.Peer("p1"), ss, &x, &y)
		require.NoError(t, ss.Sync(ctx, &x, &y))

		st.is.EXPECT().Has(addedKey)
		st.is.EXPECT().Add(ctx, addedKey)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, addedKey)
		require.NoError(t, ss.Sync(ctx, nil, nil))
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.Hash32{addedKey}, handledKeys)
	})

	t.Run("single key synced multiple times", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		addedKey := types.RandomHash()
		st.expectCopy(ctx, addedKey, addedKey, addedKey)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		// added just once
		st.is.EXPECT().Add(ctx, addedKey)
		for i := 0; i < 3; i++ {
			st.is.EXPECT().Has(addedKey)
			st.expectSyncStore(ctx, p2p.Peer("p1"), ss, addedKey)
			require.NoError(t, ss.Sync(ctx, nil, nil))
		}
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.Hash32{addedKey}, handledKeys)
	})

	t.Run("multiple keys", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		k1 := types.RandomHash()
		k2 := types.RandomHash()
		st.expectCopy(ctx, k1, k2)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		st.is.EXPECT().Has(k1)
		st.is.EXPECT().Has(k2)
		st.is.EXPECT().Add(ctx, k1)
		st.is.EXPECT().Add(ctx, k2)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, k1, k2)
		require.NoError(t, ss.Sync(ctx, nil, nil))
		close(st.getWaitCh(k1))
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.Hash32{k1, k2}, handledKeys)
	})

	t.Run("handler failure", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		ctx := context.Background()

		k1 := types.RandomHash()
		k2 := types.RandomHash()
		st.expectCopy(ctx, k1, k2)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		st.is.EXPECT().Has(k1)
		st.is.EXPECT().Has(k2)
		// k1 is not propagated to syncBase due to the handler failure
		st.is.EXPECT().Add(ctx, k2)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, k1, k2)
		require.NoError(t, ss.Sync(ctx, nil, nil))
		handlerErr := errors.New("fail")
		st.getWaitCh(k1) <- handlerErr
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, []types.Hash32{k1, k2}, handledKeys)
	})

	t.Run("synctree based item store", func(t *testing.T) {
		t.Parallel()
		hs := make([]types.Hash32, 4)
		for n := range hs {
			hs[n] = types.RandomHash()
		}
		is := NewSyncTreeStore(Hash32To12Xor{})
		is.Add(context.Background(), hs[0])
		is.Add(context.Background(), hs[1])
		st := newSetSyncBaseTester(t, is)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		ss.(ItemStore).Add(context.Background(), hs[2])
		ss.(ItemStore).Add(context.Background(), hs[3])
		// syncer's cloned ItemStore has new key immediately
		require.True(t, ss.(ItemStore).Has(hs[2]))
		require.True(t, ss.(ItemStore).Has(hs[3]))
		handlerErr := errors.New("fail")
		st.getWaitCh(hs[2]) <- handlerErr
		close(st.getWaitCh(hs[3]))
		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, hs[2:], handledKeys)
		// only successfully handled key propagate the syncBase
		require.False(t, is.Has(hs[2]))
		require.True(t, is.Has(hs[3]))
	})
}
