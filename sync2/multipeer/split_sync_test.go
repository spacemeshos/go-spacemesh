package multipeer_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type splitSyncTester struct {
	testing.TB
	syncPeers     []p2p.Peer
	clock         clockwork.FakeClock
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
		TB:        t,
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
		tst.syncPeers[n] = p2p.Peer(fmt.Sprintf("peer%d", n))
	}
	for _, p := range tst.syncPeers {
		tst.peers.Add(p, func() []protocol.ID { return []protocol.ID{multipeer.Protocol} })
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

func (tst *splitSyncTester) expectPeerSync(p p2p.Peer) {
	tst.syncBase.EXPECT().
		Sync(gomock.Any(), p, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p p2p.Peer, x, y rangesync.KeyBytes) error {
			tst.mtx.Lock()
			defer tst.mtx.Unlock()
			require.NotNil(tst, x)
			require.NotNil(tst, y)
			k := hexRange{x.String(), y.String()}
			tst.peerRanges[k] = append(tst.peerRanges[k], p)
			count, found := tst.expPeerRanges[k]
			require.True(tst, found, "peer range not found: x %s y %s", x, y)
			if tst.fail[k] {
				tst.Logf("ERR: peer %s x %s y %s",
					string(p), x.String(), y.String())
				tst.fail[k] = false
				return errors.New("injected fault")
			} else {
				tst.Logf("OK: peer %s x %s y %s",
					string(p), x.String(), y.String())
				tst.expPeerRanges[k] = count + 1
			}
			return nil
		}).AnyTimes()
}

func TestSplitSync(t *testing.T) {
	tst := newTestSplitSync(t)
	for _, p := range tst.syncPeers {
		tst.expectPeerSync(p)
	}
	var eg errgroup.Group
	eg.Go(func() error {
		return tst.splitSync.Sync(context.Background())
	})
	require.NoError(t, eg.Wait())
	for pr, count := range tst.expPeerRanges {
		require.Equal(t, 1, count, "bad sync count: x %s y %s", pr[0], pr[1])
	}
}

func TestSplitSync_Retry(t *testing.T) {
	tst := newTestSplitSync(t)
	for _, p := range tst.syncPeers {
		tst.expectPeerSync(p)
	}
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

func TestSplitSync_SlowPeers(t *testing.T) {
	tst := newTestSplitSync(t)

	for _, p := range tst.syncPeers[:2] {
		tst.expectPeerSync(p)
	}

	for _, p := range tst.syncPeers[2:] {
		tst.syncBase.EXPECT().
			Sync(gomock.Any(), p, gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, p p2p.Peer, x, y rangesync.KeyBytes) error {
				<-ctx.Done()
				return nil
			})
	}

	var eg errgroup.Group
	eg.Go(func() error {
		return tst.splitSync.Sync(context.Background())
	})

	require.Eventually(t, func() bool {
		tst.mtx.Lock()
		defer tst.mtx.Unlock()
		return len(tst.peerRanges) == 2
	}, 10*time.Millisecond, time.Millisecond)
	// Make sure all 4 grace period timers are started.
	tst.clock.BlockUntil(4)
	tst.clock.Advance(time.Minute)
	require.NoError(t, eg.Wait())
	for pr, count := range tst.expPeerRanges {
		require.Equal(t, 1, count, "bad sync count: x %s y %s", pr[0], pr[1])
	}
}
