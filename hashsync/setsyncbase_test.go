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
	t       *testing.T
	ctrl    *gomock.Controller
	ps      *MockpairwiseSyncer
	is      *MockItemStore
	ssb     *setSyncBase
	waitMtx sync.Mutex
	waitChs map[Ordered]chan error
	doneCh  chan Ordered
}

func newSetSyncBaseTester(t *testing.T) *setSyncBaseTester {
	ctrl := gomock.NewController(t)
	st := &setSyncBaseTester{
		t:       t,
		ctrl:    ctrl,
		ps:      NewMockpairwiseSyncer(ctrl),
		is:      NewMockItemStore(ctrl),
		waitChs: make(map[Ordered]chan error),
		doneCh:  make(chan Ordered),
	}
	st.ssb = newSetSyncBase(st.ps, st.is, func(ctx context.Context, k Ordered) error {
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

func (st *setSyncBaseTester) expectCopy(ctx context.Context, addedKeys ...types.Hash32) {
	st.is.EXPECT().Copy().DoAndReturn(func() ItemStore {
		copy := NewMockItemStore(st.ctrl)
		for _, k := range addedKeys {
			copy.EXPECT().Add(ctx, k)
		}
		return copy
	})
}

func (st *setSyncBaseTester) expectSyncStore(
	ctx context.Context,
	p p2p.Peer,
	ss syncer,
	addedKeys ...types.Hash32,
) {
	st.ps.EXPECT().syncStore(ctx, p, ss, nil, nil).
		DoAndReturn(func(ctx context.Context, p p2p.Peer, is ItemStore, x, y *types.Hash32) error {
			for _, k := range addedKeys {
				require.NoError(st.t, is.Add(ctx, k))
			}
			return nil
		})
}

func (st *setSyncBaseTester) failToSyncStore(
	ctx context.Context,
	p p2p.Peer,
	ss syncer,
	err error,
) {
	st.ps.EXPECT().syncStore(ctx, p, ss, nil, nil).
		DoAndReturn(func(ctx context.Context, p p2p.Peer, is ItemStore, x, y *types.Hash32) error {
			return err
		})
}

func (st *setSyncBaseTester) wait(count int) ([]types.Hash32, error) {
	var eg errgroup.Group
	eg.Go(st.ssb.wait)
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
		st := newSetSyncBaseTester(t)
		ctx := context.Background()
		expPr := ProbeResult{
			FP:    types.RandomHash(),
			Count: 42,
			Sim:   0.99,
		}
		st.ps.EXPECT().probe(ctx, p2p.Peer("p1"), st.is, nil, nil).Return(expPr, nil)
		pr, err := st.ssb.probe(ctx, p2p.Peer("p1"))
		require.NoError(t, err)
		require.Equal(t, expPr, pr)
	})

	t.Run("single key one-time sync", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t)
		ctx := context.Background()

		addedKey := types.RandomHash()
		st.expectCopy(ctx, addedKey)
		ss := st.ssb.derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.peer())

		x := types.RandomHash()
		y := types.RandomHash()
		st.ps.EXPECT().syncStore(ctx, p2p.Peer("p1"), ss, &x, &y)
		require.NoError(t, ss.sync(ctx, &x, &y))

		st.is.EXPECT().Has(addedKey)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, addedKey)
		require.NoError(t, ss.sync(ctx, nil, nil))
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.Hash32{addedKey}, handledKeys)
	})

	t.Run("single key synced multiple times", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t)
		ctx := context.Background()

		addedKey := types.RandomHash()
		st.expectCopy(ctx, addedKey, addedKey, addedKey)
		ss := st.ssb.derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.peer())

		for i := 0; i < 3; i++ {
			st.is.EXPECT().Has(addedKey)
			st.expectSyncStore(ctx, p2p.Peer("p1"), ss, addedKey)
			require.NoError(t, ss.sync(ctx, nil, nil))
		}
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.Hash32{addedKey}, handledKeys)
	})

	t.Run("multiple keys", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t)
		ctx := context.Background()

		k1 := types.RandomHash()
		k2 := types.RandomHash()
		st.expectCopy(ctx, k1, k2)
		ss := st.ssb.derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.peer())

		st.is.EXPECT().Has(k1)
		st.is.EXPECT().Has(k2)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, k1, k2)
		require.NoError(t, ss.sync(ctx, nil, nil))
		close(st.getWaitCh(k1))
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.Hash32{k1, k2}, handledKeys)
	})

	t.Run("handler failure", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t)
		ctx := context.Background()

		k1 := types.RandomHash()
		k2 := types.RandomHash()
		st.expectCopy(ctx, k1, k2)
		ss := st.ssb.derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.peer())

		st.is.EXPECT().Has(k1)
		st.is.EXPECT().Has(k2)
		st.expectSyncStore(ctx, p2p.Peer("p1"), ss, k1, k2)
		require.NoError(t, ss.sync(ctx, nil, nil))
		handlerErr := errors.New("fail")
		st.getWaitCh(k1) <- handlerErr
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, []types.Hash32{k1, k2}, handledKeys)
	})
}
