package hashsync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"
)

func hexDelimiters(n int) (r []string) {
	for _, h := range getDelimiters(n) {
		r = append(r, h.Hex())
	}
	return r
}

func TestGetDelimiters(t *testing.T) {
	for _, tc := range []struct {
		numPeers int
		values   []string
	}{
		{
			numPeers: 0,
			values:   nil,
		},
		{
			numPeers: 1,
			values:   nil,
		},
		{
			numPeers: 2,
			values: []string{
				"0x8000000000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 3,
			values: []string{
				"0x5555555555555554000000000000000000000000000000000000000000000000",
				"0xaaaaaaaaaaaaaaa8000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 4,
			values: []string{
				"0x4000000000000000000000000000000000000000000000000000000000000000",
				"0x8000000000000000000000000000000000000000000000000000000000000000",
				"0xc000000000000000000000000000000000000000000000000000000000000000",
			},
		},
	} {
		r := hexDelimiters(tc.numPeers)
		if len(tc.values) == 0 {
			require.Empty(t, r, "%d delimiters", tc.numPeers)
		} else {
			require.Equal(t, tc.values, r, "%d delimiters", tc.numPeers)
		}
	}
}

type splitSyncTester struct {
	testing.TB

	peers         []p2p.Peer
	clock         clockwork.Clock
	mtx           sync.Mutex
	fail          map[hexRange]bool
	expPeerRanges map[hexRange]int
	peerRanges    map[hexRange][]p2p.Peer
	syncBase      *MocksyncBase
	peerSet       *MockpeerSet
	splitSync     *splitSync
}

var tstRanges = []hexRange{
	{
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		"0x4000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"0x4000000000000000000000000000000000000000000000000000000000000000",
		"0x8000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"0x8000000000000000000000000000000000000000000000000000000000000000",
		"0xc000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"0xc000000000000000000000000000000000000000000000000000000000000000",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
	},
}

func newTestSplitSync(t testing.TB) *splitSyncTester {
	ctrl := gomock.NewController(t)
	tst := &splitSyncTester{
		peers: make([]p2p.Peer, 4),
		clock: clockwork.NewFakeClock(),
		fail:  make(map[hexRange]bool),
		expPeerRanges: map[hexRange]int{
			tstRanges[0]: 0,
			tstRanges[1]: 0,
			tstRanges[2]: 0,
			tstRanges[3]: 0,
		},
		peerRanges: make(map[hexRange][]p2p.Peer),
		syncBase:   NewMocksyncBase(ctrl),
		peerSet:    NewMockpeerSet(ctrl),
	}
	for n := range tst.peers {
		tst.peers[n] = p2p.Peer(types.RandomBytes(20))
	}
	for index, p := range tst.peers {
		index := index
		p := p
		tst.syncBase.EXPECT().
			derive(p).
			DoAndReturn(func(peer p2p.Peer) syncer {
				s := NewMocksyncer(ctrl)
				s.EXPECT().peer().Return(p).AnyTimes()
				s.EXPECT().
					sync(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, x, y *types.Hash32) error {
						tst.mtx.Lock()
						defer tst.mtx.Unlock()
						require.NotNil(t, ctx)
						require.NotNil(t, x)
						require.NotNil(t, y)
						k := hexRange{x.Hex(), y.Hex()}
						tst.peerRanges[k] = append(tst.peerRanges[k], peer)
						count, found := tst.expPeerRanges[k]
						require.True(t, found, "peer range not found: x %s y %s", x, y)
						if tst.fail[k] {
							t.Logf("ERR: peer %d x %s y %s", index, x.String(), y.String())
							tst.fail[k] = false
							return errors.New("injected fault")
						} else {
							t.Logf("OK: peer %d x %s y %s", index, x.String(), y.String())
							tst.expPeerRanges[k] = count + 1
						}
						return nil
					})
				return s
			}).
			AnyTimes()
	}
	tst.peerSet.EXPECT().
		havePeer(gomock.Any()).
		DoAndReturn(func(p p2p.Peer) bool {
			require.Contains(t, tst.peers, p)
			return true
		}).
		AnyTimes()
	tst.splitSync = newSplitSync(
		zaptest.NewLogger(t),
		tst.syncBase,
		tst.peerSet,
		tst.peers,
		time.Minute,
		tst.clock,
	)
	return tst
}

func TestSplitSync(t *testing.T) {
	tst := newTestSplitSync(t)
	var eg errgroup.Group
	eg.Go(func() error {
		return tst.splitSync.sync(context.Background())
	})
	require.NoError(t, eg.Wait())
	for pr, count := range tst.expPeerRanges {
		require.Equal(t, 1, count, "bad sync count: x %s y %s", pr[0], pr[1])
	}
}

func TestSplitSyncRetry(t *testing.T) {
	tst := newTestSplitSync(t)
	tst.fail[tstRanges[1]] = true
	tst.fail[tstRanges[2]] = true
	removedPeers := make(map[p2p.Peer]bool)
	tst.peerSet.EXPECT().removePeer(gomock.Any()).DoAndReturn(func(peer p2p.Peer) {
		require.NotContains(t, removedPeers, peer)
		removedPeers[peer] = true
	}).Times(2)
	var eg errgroup.Group
	eg.Go(func() error {
		return tst.splitSync.sync(context.Background())
	})
	require.NoError(t, eg.Wait())
	for pr, count := range tst.expPeerRanges {
		require.False(t, tst.fail[pr], "fail cleared for x %s y %s", pr[0], pr[1])
		require.Equal(t, 1, count, "peer range not synced: x %s y %s", pr[0], pr[1])
	}
	for _, r := range []hexRange{tstRanges[1], tstRanges[2]} {
		haveFailedPeers := false
		for _, peer := range tst.peerRanges[r] {
			if removedPeers[peer] {
				haveFailedPeers = true
			}
		}
		require.True(t, haveFailedPeers)
	}
}

// TODO: test cancel
// TODO: test sync failure
// TODO: test out of peers due to failure
// TODO: test dropping failed peers (there should be a hook so that the peer connection is terminated)
// TODO: log peer sync failures
// TODO: log sync starts
// TODO: log overlapping syncs
