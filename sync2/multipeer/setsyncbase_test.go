package multipeer_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync/mocks"
)

type setSyncBaseTester struct {
	*testing.T
	ctrl    *gomock.Controller
	ps      *MockPairwiseSyncer
	handler *MockSyncKeyHandler
	os      *mocks.MockOrderedSet
	ssb     *multipeer.SetSyncBase
	waitMtx sync.Mutex
	waitChs map[string]chan error
	doneCh  chan rangesync.KeyBytes
}

func newSetSyncBaseTester(t *testing.T, os rangesync.OrderedSet) *setSyncBaseTester {
	ctrl := gomock.NewController(t)
	st := &setSyncBaseTester{
		T:       t,
		ctrl:    ctrl,
		ps:      NewMockPairwiseSyncer(ctrl),
		waitChs: make(map[string]chan error),
		doneCh:  make(chan rangesync.KeyBytes),
	}
	if os == nil {
		st.os = mocks.NewMockOrderedSet(ctrl)
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
	st.ssb = multipeer.NewSetSyncBase(zaptest.NewLogger(t), st.ps, os, st.handler)
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

func (st *setSyncBaseTester) expectCopy(addedKeys ...rangesync.KeyBytes) *mocks.MockOrderedSet {
	copy := mocks.NewMockOrderedSet(st.ctrl)
	st.os.EXPECT().WithCopy(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, toCall func(rangesync.OrderedSet) error) error {
			copy.EXPECT().Items().DoAndReturn(func() rangesync.SeqResult {
				return rangesync.EmptySeqResult()
			}).AnyTimes()
			for _, k := range addedKeys {
				copy.EXPECT().Receive(k)
			}
			return toCall(copy)
		})
	return copy
}

func (st *setSyncBaseTester) expectSync(
	p p2p.Peer,
	ss multipeer.PeerSyncer,
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
		require.NoError(t, st.ssb.WithPeerSyncer(
			context.Background(), p2p.Peer("p1"),
			func(ps multipeer.PeerSyncer) error {
				require.Equal(t, p2p.Peer("p1"), ps.Peer())

				x := rangesync.RandomKeyBytes(32)
				y := rangesync.RandomKeyBytes(32)
				st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				st.os.EXPECT().Advance()
				st.ps.EXPECT().Sync(gomock.Any(), p2p.Peer("p1"), ps, x, y)
				require.NoError(t, ps.Sync(context.Background(), x, y))

				st.os.EXPECT().Has(addedKey)
				st.os.EXPECT().Receive(addedKey)
				st.expectSync(p2p.Peer("p1"), ps, addedKey)
				st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				st.os.EXPECT().Advance()
				require.NoError(t, ps.Sync(context.Background(), nil, nil))
				close(st.getWaitCh(addedKey))
				return nil
			}))

		handledKeys, err := st.wait(1)
		require.NoError(t, err)
		require.ElementsMatch(t, []rangesync.KeyBytes{addedKey}, handledKeys)
	})

	t.Run("single key synced multiple times", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		addedKey := rangesync.RandomKeyBytes(32)
		st.expectCopy(addedKey, addedKey, addedKey)
		require.NoError(t, st.ssb.WithPeerSyncer(
			context.Background(), p2p.Peer("p1"),
			func(ps multipeer.PeerSyncer) error {
				require.Equal(t, p2p.Peer("p1"), ps.Peer())
				// added just once
				st.os.EXPECT().Receive(addedKey)
				for i := 0; i < 3; i++ {
					st.os.EXPECT().Has(addedKey)
					st.expectSync(p2p.Peer("p1"), ps, addedKey)
					st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
					st.os.EXPECT().Advance()
					require.NoError(t, ps.Sync(context.Background(), nil, nil))
				}
				close(st.getWaitCh(addedKey))
				return nil
			}))

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
		require.NoError(t, st.ssb.WithPeerSyncer(
			context.Background(), p2p.Peer("p1"),
			func(ps multipeer.PeerSyncer) error {
				require.Equal(t, p2p.Peer("p1"), ps.Peer())

				st.os.EXPECT().Has(k1)
				st.os.EXPECT().Has(k2)
				st.os.EXPECT().Receive(k1)
				st.os.EXPECT().Receive(k2)
				st.expectSync(p2p.Peer("p1"), ps, k1, k2)
				st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				st.os.EXPECT().Advance()
				require.NoError(t, ps.Sync(context.Background(), nil, nil))
				close(st.getWaitCh(k1))
				close(st.getWaitCh(k2))
				return nil
			}))
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
		require.NoError(t, st.ssb.WithPeerSyncer(
			context.Background(), p2p.Peer("p1"),
			func(ps multipeer.PeerSyncer) error {
				require.Equal(t, p2p.Peer("p1"), ps.Peer())

				st.os.EXPECT().Has(k1)
				st.os.EXPECT().Has(k2)
				// k1 is not propagated to syncBase due to the handler failure
				st.os.EXPECT().Receive(k2)
				st.expectSync(p2p.Peer("p1"), ps, k1, k2)
				st.handler.EXPECT().Commit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				st.os.EXPECT().Advance()
				require.NoError(t, ps.Sync(context.Background(), nil, nil))
				st.getWaitCh(k1) <- errors.New("fail")
				close(st.getWaitCh(k2))
				return nil
			}))

		handledKeys, err := st.wait(2)
		require.ErrorContains(t, err, "some key handlers failed")
		require.ElementsMatch(t, []rangesync.KeyBytes{k1, k2}, handledKeys)
	})

	t.Run("real item set", func(t *testing.T) {
		t.Parallel()
		hs := make([]rangesync.KeyBytes, 4)
		for n := range hs {
			hs[n] = rangesync.RandomKeyBytes(32)
		}
		var os rangesync.DumbSet
		os.AddUnchecked(hs[0])
		os.AddUnchecked(hs[1])
		st := newSetSyncBaseTester(t, &os)
		require.NoError(t, st.ssb.WithPeerSyncer(
			context.Background(), p2p.Peer("p1"),
			func(ps multipeer.PeerSyncer) error {
				ps.(rangesync.OrderedSet).Receive(hs[2])
				ps.(rangesync.OrderedSet).Add(hs[2])
				ps.(rangesync.OrderedSet).Receive(hs[3])
				ps.(rangesync.OrderedSet).Add(hs[3])
				// syncer's cloned set has new key immediately
				has, err := ps.(rangesync.OrderedSet).Has(hs[2])
				require.NoError(t, err)
				require.True(t, has)
				has, err = ps.(rangesync.OrderedSet).Has(hs[3])
				require.NoError(t, err)
				require.True(t, has)
				return nil
			}))
		st.getWaitCh(hs[2]) <- errors.New("fail")
		close(st.getWaitCh(hs[3]))
		handledKeys, err := st.wait(2)
		require.ErrorContains(t, err, "some key handlers failed")
		require.ElementsMatch(t, hs[2:], handledKeys)
		// only successfully handled keys propagate the syncBase
		received, err := os.Received().Collect()
		require.NoError(t, err)
		require.ElementsMatch(t, hs[3:], received)
	})
}
