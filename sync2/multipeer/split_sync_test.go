package multipeer_test

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
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

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
	splitSync     *multipeer.SplitSync
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
		tst.syncBase.EXPECT().
			Derive(p).
			DoAndReturn(func(peer p2p.Peer) multipeer.Syncer {
				s := NewMockSyncer(ctrl)
				s.EXPECT().Peer().Return(p).AnyTimes()
				// TODO: do better job at tracking Release() calls
				s.EXPECT().Release().AnyTimes()
				s.EXPECT().
					Sync(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, x, y rangesync.KeyBytes) error {
						tst.mtx.Lock()
						defer tst.mtx.Unlock()
						require.NotNil(t, x)
						require.NotNil(t, y)
						k := hexRange{x.String(), y.String()}
						tst.peerRanges[k] = append(tst.peerRanges[k], peer)
						count, found := tst.expPeerRanges[k]
						require.True(t, found, "peer range not found: x %s y %s", x, y)
						if tst.fail[k] {
							t.Logf("ERR: peer %d x %s y %s",
								index, x.String(), y.String())
							tst.fail[k] = false
							return errors.New("injected fault")
						} else {
							t.Logf("OK: peer %d x %s y %s",
								index, x.String(), y.String())
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
	tst.splitSync = multipeer.NewSplitSync(
		zaptest.NewLogger(t),
		tst.syncBase,
		tst.peers,
		tst.syncPeers,
		time.Minute,
		tst.clock,
		32, 24,
	)
	return tst
}

func TestSplitSync(t *testing.T) {
	tst := newTestSplitSync(t)
	var eg errgroup.Group
	eg.Go(func() error {
		return tst.splitSync.Sync(context.Background())
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
		return tst.splitSync.Sync(context.Background())
	})
	require.NoError(t, eg.Wait())
	for pr, count := range tst.expPeerRanges {
		require.False(t, tst.fail[pr], "fail cleared for x %s y %s", pr[0], pr[1])
		require.Equal(t, 1, count, "peer range not synced: x %s y %s", pr[0], pr[1])
	}
}
