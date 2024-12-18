package multipeer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

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
}

func newSetSyncBaseTester(t *testing.T, os rangesync.OrderedSet) *setSyncBaseTester {
	ctrl := gomock.NewController(t)
	st := &setSyncBaseTester{
		T:    t,
		ctrl: ctrl,
		ps:   NewMockPairwiseSyncer(ctrl),
	}
	if os == nil {
		st.os = mocks.NewMockOrderedSet(ctrl)
		os = st.os
	}
	st.handler = NewMockSyncKeyHandler(ctrl)
	st.ssb = multipeer.NewSetSyncBase(st.ps, os, st.handler)
	return st
}

func (st *setSyncBaseTester) expectCopy() *mocks.MockOrderedSet {
	copy := mocks.NewMockOrderedSet(st.ctrl)
	st.os.EXPECT().WithCopy(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, toCall func(rangesync.OrderedSet) error) error {
			return toCall(copy)
		})
	return copy
}

func TestSetSyncBase(t *testing.T) {
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)
		expPr := rangesync.ProbeResult{
			InSync: false,
			Count:  42,
			Sim:    0.99,
		}
		set := st.expectCopy()
		st.ps.EXPECT().Probe(gomock.Any(), p2p.Peer("p1"), set, nil, nil).Return(expPr, nil)
		pr, err := st.ssb.Probe(context.Background(), p2p.Peer("p1"))
		require.NoError(t, err)
		require.Equal(t, expPr, pr)
	})

	t.Run("sync", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		os := st.expectCopy()
		x := rangesync.RandomKeyBytes(32)
		y := rangesync.RandomKeyBytes(32)
		st.ps.EXPECT().Sync(gomock.Any(), p2p.Peer("p1"), os, x, y)
		addedKeys := []rangesync.KeyBytes{rangesync.RandomKeyBytes(32)}
		sr := rangesync.MakeSeqResult(addedKeys)
		os.EXPECT().Received().Return(sr)
		st.handler.EXPECT().Commit(gomock.Any(), p2p.Peer("p1"), st.os, gomock.Any()).
			DoAndReturn(func(
				_ context.Context,
				_ p2p.Peer,
				_ rangesync.OrderedSet,
				sr rangesync.SeqResult,
			) error {
				items, err := sr.Collect()
				require.NoError(t, err)
				require.ElementsMatch(t, addedKeys, items)
				return nil
			})
		st.os.EXPECT().Advance()
		st.ssb.Sync(context.Background(), p2p.Peer("p1"), x, y)
	})

	t.Run("count empty", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		st.os.EXPECT().Empty().Return(true, nil)
		count, err := st.ssb.Count()
		require.NoError(t, err)
		require.Zero(t, count)
	})

	t.Run("count non-empty", func(t *testing.T) {
		t.Parallel()
		st := newSetSyncBaseTester(t, nil)

		st.os.EXPECT().Empty().Return(false, nil)
		items := []rangesync.KeyBytes{
			rangesync.RandomKeyBytes(32),
			rangesync.RandomKeyBytes(32),
		}
		st.os.EXPECT().Items().Return(rangesync.MakeSeqResult(items))
		st.os.EXPECT().GetRangeInfo(items[0], items[0]).Return(rangesync.RangeInfo{Count: 2}, nil)
		count, err := st.ssb.Count()
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})
}
