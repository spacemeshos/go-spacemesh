package multipeer_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type setSyncBaseTester struct {
	*testing.T
	ctrl    *gomock.Controller
	ps      *MockPairwiseSyncer
	handler *MockSyncKeyHandler
	os      *MockOrderedSet
	ssb     *multipeer.SetSyncBase
	waitMtx sync.Mutex
	waitChs map[string]chan error
	doneCh  chan rangesync.KeyBytes
}

func newSetSyncBaseTester(t *testing.T, os multipeer.OrderedSet) *setSyncBaseTester {
	ctrl := gomock.NewController(t)
	st := &setSyncBaseTester{
		T:       t,
		ctrl:    ctrl,
		ps:      NewMockPairwiseSyncer(ctrl),
		waitChs: make(map[string]chan error),
		doneCh:  make(chan rangesync.KeyBytes),
	}
	if os == nil {
		st.os = NewMockOrderedSet(ctrl)
		st.os.EXPECT().Items().DoAndReturn(func() rangesync.SeqResult {
			return rangesync.EmptySeqResult()
		}).AnyTimes()
		os = st.os
	}
	st.handler = NewMockSyncKeyHandler(ctrl)
	st.handler.EXPECT().Receive(gomock.Any(), gomock.Any()).
		DoAndReturn(func(k rangesync.KeyBytes, p p2p.Peer) (bool, error) {
			err := <-st.getWaitCh(k)
			st.doneCh <- k
			return true, err
		}).AnyTimes()
	st.ssb = multipeer.NewSetSyncBase(st.ps, os, st.handler)
	return st
}

func (st *setSyncBaseTester) getWaitCh(k rangesync.KeyBytes) chan error {
	st.waitMtx.Lock()
	defer st.waitMtx.Unlock()
	ch, found := st.waitChs[string(k)]
	if !found {
		ch = make(chan error)
		st.waitChs[string(k)] = ch
	}
	return ch
}

func (st *setSyncBaseTester) expectCopy(addedKeys ...rangesync.KeyBytes) *MockOrderedSet {
	copy := NewMockOrderedSet(st.ctrl)
	st.os.EXPECT().Copy(true).DoAndReturn(func(bool) rangesync.OrderedSet {
		copy.EXPECT().Items().DoAndReturn(func() rangesync.SeqResult {
			return rangesync.EmptySeqResult()
		}).AnyTimes()
		for _, k := range addedKeys {
			copy.EXPECT().Receive(k)
		}
		// TODO: do better job at tracking Release() calls
		copy.EXPECT().Release().AnyTimes()
		return copy
	})
	return copy
}

func (st *setSyncBaseTester) expectSync(
	p p2p.Peer,
	ss multipeer.Syncer,
	addedKeys ...rangesync.KeyBytes,
) {
	st.ps.EXPECT().Sync(gomock.Any(), p, ss, nil, nil).
		DoAndReturn(func(
			_ context.Context,
			p p2p.Peer,
			os rangesync.OrderedSet,
			x, y rangesync.KeyBytes,
		) error {
			for _, k := range addedKeys {
				require.NoError(st, os.Receive(k))
			}
			return nil
		})
}

func (st *setSyncBaseTester) wait(count int) ([]rangesync.KeyBytes, error) {
	var eg errgroup.Group
	eg.Go(st.ssb.Wait)
	var handledKeys []rangesync.KeyBytes
	for k := range st.doneCh {
		handledKeys = append(handledKeys, k.Clone())
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
		expPr := rangesync.ProbeResult{
			FP:    rangesync.RandomFingerprint(),
			Count: 42,
			Sim:   0.99,
		}
		set := st.expectCopy()
		st.ps.EXPECT().Probe(gomock.Any(), p2p.Peer("p1"), set, nil, nil).Return(expPr, nil)
		pr, err := st.ssb.Probe(context.Background(), p2p.Peer("p1"))
		require.NoError(t, err)
		require.Equal(t, expPr, pr)
	})

	t.Run("single key one-time sync", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		addedKey := rangesync.RandomKeyBytes(32)
		st.expectCopy(addedKey)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		x := rangesync.RandomKeyBytes(32)
		y := rangesync.RandomKeyBytes(32)
		st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any())
		st.os.EXPECT().Advance()
		st.ps.EXPECT().Sync(gomock.Any(), p2p.Peer("p1"), ss, x, y)
		require.NoError(t, ss.Sync(context.Background(), x, y))

		st.os.EXPECT().Has(addedKey)
		st.os.EXPECT().Receive(addedKey)
		st.expectSync(p2p.Peer("p1"), ss, addedKey)
		st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any())
		st.os.EXPECT().Advance()
		require.NoError(t, ss.Sync(context.Background(), nil, nil))
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []rangesync.KeyBytes{addedKey}, handledKeys)
	})

	t.Run("single key synced multiple times", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		addedKey := rangesync.RandomKeyBytes(32)
		st.expectCopy(addedKey, addedKey, addedKey)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		// added just once
		st.os.EXPECT().Receive(addedKey)
		for i := 0; i < 3; i++ {
			st.os.EXPECT().Has(addedKey)
			st.expectSync(p2p.Peer("p1"), ss, addedKey)
			st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any())
			st.os.EXPECT().Advance()
			require.NoError(t, ss.Sync(context.Background(), nil, nil))
		}
		close(st.getWaitCh(addedKey))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []rangesync.KeyBytes{addedKey}, handledKeys)
	})

	t.Run("multiple keys", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		k1 := rangesync.RandomKeyBytes(32)
		k2 := rangesync.RandomKeyBytes(32)
		st.expectCopy(k1, k2)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		st.os.EXPECT().Has(k1)
		st.os.EXPECT().Has(k2)
		st.os.EXPECT().Receive(k1)
		st.os.EXPECT().Receive(k2)
		st.expectSync(p2p.Peer("p1"), ss, k1, k2)
		st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any())
		st.os.EXPECT().Advance()
		require.NoError(t, ss.Sync(context.Background(), nil, nil))
		close(st.getWaitCh(k1))
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.NoError(t, err)
		require.ElementsMatch(t, []rangesync.KeyBytes{k1, k2}, handledKeys)
	})

	t.Run("handler failure", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		k1 := rangesync.RandomKeyBytes(32)
		k2 := rangesync.RandomKeyBytes(32)
		st.expectCopy(k1, k2)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		require.Equal(t, p2p.Peer("p1"), ss.Peer())

		st.os.EXPECT().Has(k1)
		st.os.EXPECT().Has(k2)
		// k1 is not propagated to syncBase due to the handler failure
		st.os.EXPECT().Receive(k2)
		st.expectSync(p2p.Peer("p1"), ss, k1, k2)
		st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any())
		st.os.EXPECT().Advance()
		require.NoError(t, ss.Sync(context.Background(), nil, nil))
		handlerErr := errors.New("fail")
		st.getWaitCh(k1) <- handlerErr
		close(st.getWaitCh(k2))

		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, []rangesync.KeyBytes{k1, k2}, handledKeys)
	})

	t.Run("real item set", func(t *testing.T) {
		t.Parallel()
		hs := make([]rangesync.KeyBytes, 4)
		for n := range hs {
			hs[n] = rangesync.RandomKeyBytes(32)
		}
		os := multipeer.NewDumbHashSet()
		os.AddUnchecked(hs[0])
		os.AddUnchecked(hs[1])
		st := newSetSyncBaseTester(t, os)
		ss := st.ssb.Derive(p2p.Peer("p1"))
		ss.(rangesync.OrderedSet).Receive(hs[2])
		ss.(rangesync.OrderedSet).Add(hs[2])
		ss.(rangesync.OrderedSet).Receive(hs[3])
		ss.(rangesync.OrderedSet).Add(hs[3])
		// syncer's cloned ItemStore has new key immediately
		has, err := ss.(multipeer.OrderedSet).Has(hs[2])
		require.NoError(t, err)
		require.True(t, has)
		has, err = ss.(multipeer.OrderedSet).Has(hs[3])
		require.NoError(t, err)
		require.True(t, has)
		handlerErr := errors.New("fail")
		st.getWaitCh(hs[2]) <- handlerErr
		close(st.getWaitCh(hs[3]))
		handledKeys, err := st.wait(2)
		require.ErrorIs(t, err, handlerErr)
		require.ElementsMatch(t, hs[2:], handledKeys)
		// only successfully handled keys propagate the syncBase
		received, err := os.Received().Collect()
		require.NoError(t, err)
		require.ElementsMatch(t, hs[3:], received)
	})
}
