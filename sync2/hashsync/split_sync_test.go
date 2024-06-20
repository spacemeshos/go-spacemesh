package hashsync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

func hexDelimiters(n int) (r []string) {
	for _, h := range getDelimiters(n) {
		r = append(r, h.String())
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
				"8000000000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 3,
			values: []string{
				"5555555555555554000000000000000000000000000000000000000000000000",
				"aaaaaaaaaaaaaaa8000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 4,
			values: []string{
				"4000000000000000000000000000000000000000000000000000000000000000",
				"8000000000000000000000000000000000000000000000000000000000000000",
				"c000000000000000000000000000000000000000000000000000000000000000",
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

	syncPeers     []p2p.Peer
	clock         clockwork.Clock
	mtx           sync.Mutex
	fail          map[hexRange]bool
	expPeerRanges map[hexRange]int
	peerRanges    map[hexRange][]p2p.Peer
	syncBase      *MockSyncBase
	peers         *peers.Peers
	splitSync     *splitSync
}

var tstRanges = []hexRange{
	{
		"0000000000000000000000000000000000000000000000000000000000000000",
		"4000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"4000000000000000000000000000000000000000000000000000000000000000",
		"8000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"8000000000000000000000000000000000000000000000000000000000000000",
		"c000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"c000000000000000000000000000000000000000000000000000000000000000",
		"0000000000000000000000000000000000000000000000000000000000000000",
	},
}

func newTestSplitSync(t testing.TB) *splitSyncTester {
	ctrl := gomock.NewController(t)
	tst := &splitSyncTester{
		syncPeers: make([]p2p.Peer, 4),
		clock:     clockwork.NewFakeClock(),
		fail:      make(map[hexRange]bool),
		expPeerRanges: map[hexRange]int{
			tstRanges[0]: 0,
			tstRanges[1]: 0,
			tstRanges[2]: 0,
			tstRanges[3]: 0,
		},
		peerRanges: make(map[hexRange][]p2p.Peer),
		syncBase:   NewMockSyncBase(ctrl),
		peers:      peers.New(),
	}
	for n := range tst.syncPeers {
		tst.syncPeers[n] = p2p.Peer(types.RandomBytes(20))
	}
	for index, p := range tst.syncPeers {
		index := index
		p := p
		tst.syncBase.EXPECT().
			Derive(p).
			DoAndReturn(func(peer p2p.Peer) Syncer {
				s := NewMockSyncer(ctrl)
				s.EXPECT().Peer().Return(p).AnyTimes()
				s.EXPECT().
					Sync(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, x, y *types.Hash32) error {
						tst.mtx.Lock()
						defer tst.mtx.Unlock()
						require.NotNil(t, ctx)
						require.NotNil(t, x)
						require.NotNil(t, y)
						k := hexRange{x.String(), y.String()}
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
	for _, p := range tst.syncPeers {
		tst.peers.Add(p)
	}
	tst.splitSync = newSplitSync(
		zaptest.NewLogger(t),
		tst.syncBase,
		tst.peers,
		tst.syncPeers,
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
	var eg errgroup.Group
	eg.Go(func() error {
		return tst.splitSync.sync(context.Background())
	})
	require.NoError(t, eg.Wait())
	for pr, count := range tst.expPeerRanges {
		require.False(t, tst.fail[pr], "fail cleared for x %s y %s", pr[0], pr[1])
		require.Equal(t, 1, count, "peer range not synced: x %s y %s", pr[0], pr[1])
	}
}

// TODO: test cancel
// TODO: test sync failure
// TODO: test out of peers due to failure
// TODO: test dropping failed peers (there should be a hook so that the peer connection is terminated)
// TODO: log peer sync failures
// TODO: log sync starts
// TODO: log overlapping syncs
