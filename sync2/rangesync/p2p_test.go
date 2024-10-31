package rangesync_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type getRequesterFunc func(
	name string,
	handler server.StreamHandler,
	peers ...rangesync.Requester,
) (
	rangesync.Requester,
	p2p.Peer,
)

type clientServerTester struct {
	client    rangesync.Requester
	srvPeerID p2p.Peer
	pss       *rangesync.PairwiseSetSyncer
}

func newClientServerTester(
	tb testing.TB,
	set rangesync.OrderedSet,
	getRequester getRequesterFunc,
	cfg rangesync.RangeSetReconcilerConfig,
	tracer rangesync.Tracer,
	clock clockwork.Clock,
) (*clientServerTester, context.Context) {
	var (
		cst clientServerTester
		srv rangesync.Requester
	)
	d := rangesync.NewDispatcher(zaptest.NewLogger(tb))
	logger := zap.NewNop()
	cst.pss = rangesync.NewPairwiseSetSyncerInternal(
		logger, nil, "test", cfg, tracer, clock)
	cst.pss.Register(d, set)
	srv, cst.srvPeerID = getRequester("srv", d.Dispatch)
	ctx := runRequester(tb, srv)
	cst.client, _ = getRequester("client", nil, srv)
	return &cst, ctx
}

func fakeRequesterGetter(t *testing.T) getRequesterFunc {
	return func(
		name string,
		handler server.StreamHandler,
		peers ...rangesync.Requester,
	) (rangesync.Requester, p2p.Peer) {
		pid := p2p.Peer(name)
		return newFakeRequester(t, pid, handler, peers...), pid
	}
}

func p2pRequesterGetter(tb testing.TB) getRequesterFunc {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(tb, err)
	proto := "itest"
	opts := []server.Opt{
		server.WithRequestSizeLimit(100_000_000),
		server.WithTimeout(10 * time.Second),
		server.WithLog(zaptest.NewLogger(tb)),
	}
	return func(
		name string,
		handler server.StreamHandler,
		peers ...rangesync.Requester,
	) (rangesync.Requester, p2p.Peer) {
		if len(peers) == 0 {
			return server.New(mesh.Hosts()[0], proto, handler, opts...), mesh.Hosts()[0].ID()
		}
		s := server.New(mesh.Hosts()[1], proto, handler, opts...)
		require.Eventually(tb, func() bool {
			for _, h := range mesh.Hosts()[0:] {
				if len(h.Mux().Protocols()) == 0 {
					return false
				}
			}
			return true
		}, time.Second, 10*time.Millisecond)
		return s, mesh.Hosts()[1].ID()
	}
}

type syncTracer struct {
	dumb          atomic.Bool
	receivedItems int
	sentItems     int
}

var _ rangesync.Tracer = &syncTracer{}

func (tr *syncTracer) OnDumbSync() {
	tr.dumb.Store(true)
}

func (tr *syncTracer) OnRecent(receivedItems, sentItems int) {
	tr.receivedItems += receivedItems
	tr.sentItems += sentItems
}

// fakeRecentSet is a wrapper around OrderedSet that keeps track of the time when each
// item was added to the set according to the specified clock.
// It is used to test recent sync.
type fakeRecentSet struct {
	rangesync.OrderedSet
	timestamps map[string]time.Time
	clock      clockwork.Clock
}

var _ rangesync.OrderedSet = &fakeRecentSet{}

var startDate = time.Date(2024, 8, 29, 18, 0, 0, 0, time.UTC)

// registerAll assigns timestamps to all the items currently in the set.
func (frs *fakeRecentSet) registerAll(_ context.Context) error {
	frs.timestamps = make(map[string]time.Time)
	t := startDate
	items, err := frs.OrderedSet.Items().Collect()
	if err != nil {
		return err
	}
	for _, v := range items {
		frs.timestamps[string(v)] = t
		t = t.Add(time.Second)
	}
	return nil
}

// Receive implements OrderedSet.
func (frs *fakeRecentSet) Receive(k rangesync.KeyBytes) error {
	if err := frs.OrderedSet.Receive(k); err != nil {
		return err
	}
	frs.timestamps[string(k)] = frs.clock.Now()
	return nil
}

// Recent implements OrderedSet.
func (frs *fakeRecentSet) Recent(since time.Time) (rangesync.SeqResult, int) {
	var items []rangesync.KeyBytes
	items, err := frs.OrderedSet.Items().Collect()
	if err != nil {
		return rangesync.ErrorSeqResult(err), 0
	}
	items = slices.DeleteFunc(items, func(k rangesync.KeyBytes) bool {
		return frs.timestamps[string(k)].Before(since)
	})
	return rangesync.SeqResult{
		Seq:   rangesync.Seq(slices.Values(items)),
		Error: rangesync.NoSeqError,
	}, len(items)
}

func testWireSync(t *testing.T, getRequester getRequesterFunc) {
	for _, tc := range []struct {
		name           string
		cfg            hashSyncTestConfig
		dumb           bool
		rCfg           func(*rangesync.RangeSetReconcilerConfig)
		advance        time.Duration
		sentRecent     bool
		receivedRecent bool
	}{
		{
			name: "non-dumb sync",
			cfg: hashSyncTestConfig{
				maxSendRange:    1,
				numTestHashes:   1000,
				minNumSpecificA: 8,
				maxNumSpecificA: 16,
				minNumSpecificB: 8,
				maxNumSpecificB: 16,
			},
			dumb: false,
		},
		{
			name: "dumb sync",
			cfg: hashSyncTestConfig{
				maxSendRange:    1,
				numTestHashes:   1000,
				minNumSpecificA: 400,
				maxNumSpecificA: 500,
				minNumSpecificB: 400,
				maxNumSpecificB: 500,
			},
			dumb: true,
		},
		{
			name: "recent sync",
			cfg: hashSyncTestConfig{
				maxSendRange:    1,
				numTestHashes:   1000,
				minNumSpecificA: 400,
				maxNumSpecificA: 500,
				minNumSpecificB: 400,
				maxNumSpecificB: 500,
			},
			dumb: false,
			rCfg: func(cfg *rangesync.RangeSetReconcilerConfig) {
				cfg.RecentTimeSpan = 990 * time.Second
			},
			advance:        1000 * time.Second,
			sentRecent:     true,
			receivedRecent: true,
		},
		{
			name: "larger sync",
			cfg: hashSyncTestConfig{
				maxSendRange:    1,
				numTestHashes:   10000,
				minNumSpecificA: 4,
				maxNumSpecificA: 100,
				minNumSpecificB: 4,
				maxNumSpecificB: 100,
			},
			dumb: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newHashSyncTester(t, tc.cfg)
			clock := clockwork.NewFakeClockAt(startDate)
			// Note that at this point, the items are already added to the sets
			// and thus fakeRecentSet.Add is not invoked for them, just underlying
			// set's Add method
			setA := &fakeRecentSet{OrderedSet: st.setA, clock: clock}
			require.NoError(t, setA.registerAll(context.Background()))
			setB := &fakeRecentSet{OrderedSet: st.setB, clock: clock}
			require.NoError(t, setB.registerAll(context.Background()))
			var tr syncTracer
			clock.Advance(tc.advance)
			cfg := st.cfg
			if tc.rCfg != nil {
				tc.rCfg(&cfg)
			}
			cst, ctx := newClientServerTester(t, setA, getRequester, cfg, &tr, clock)
			logger := zap.NewNop()
			pss := rangesync.NewPairwiseSetSyncerInternal(
				logger, cst.client, "test", cfg, &tr, clock)
			err := pss.Sync(ctx, cst.srvPeerID, setB, nil, nil)
			require.NoError(t, err)
			st.setA.AddReceived()
			st.setB.AddReceived()

			t.Logf("numSpecific: %d, bytesSent %d, bytesReceived %d",
				st.numSpecificA+st.numSpecificB,
				pss.Sent(), pss.Received())
			require.Equal(t, tc.dumb, tr.dumb.Load(), "dumb sync")
			require.Equal(t, tc.receivedRecent, tr.receivedItems > 0)
			require.Equal(t, tc.sentRecent, tr.sentItems > 0)
			st.verify(st.setA, st.setB)
		})
	}
}

func TestWireSync(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireSync(t, fakeRequesterGetter(t))
	})
	t.Run("p2p", func(t *testing.T) {
		testWireSync(t, p2pRequesterGetter(t))
	})
}

func testWireProbe(t *testing.T, getRequester getRequesterFunc) {
	st := newHashSyncTester(t, hashSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   10000,
		minNumSpecificA: 130,
		maxNumSpecificA: 130,
		minNumSpecificB: 130,
		maxNumSpecificB: 130,
	})
	var tr rangesync.NullTracer
	clock := clockwork.NewRealClock()
	logger := zap.NewNop()
	cst, ctx := newClientServerTester(t, st.setA, getRequester, st.cfg, &tr, clock)
	pss := rangesync.NewPairwiseSetSyncerInternal(logger, cst.client, "test", st.cfg, &tr, clock)
	itemsA := st.setA.Items()
	x, err := itemsA.First()
	require.NoError(t, err)
	infoA, err := st.setA.GetRangeInfo(x, x)
	require.NoError(t, err)
	prA, err := pss.Probe(ctx, cst.srvPeerID, st.setB, nil, nil)
	require.NoError(t, err)
	require.Equal(t, infoA.Fingerprint, prA.FP)
	require.Equal(t, infoA.Count, prA.Count)
	require.InDelta(t, 0.98, prA.Sim, 0.05, "sim")

	splitInfo, err := st.setA.SplitRange(x, x, infoA.Count/2)
	require.NoError(t, err)
	prA, err = pss.Probe(ctx, cst.srvPeerID, st.setB, x, splitInfo.Middle)
	require.NoError(t, err)
	require.Equal(t, splitInfo.Parts[0].Fingerprint, prA.FP)
	require.Equal(t, splitInfo.Parts[0].Count, prA.Count)
	require.InDelta(t, 0.98, prA.Sim, 0.1, "sim")
}

func TestWireProbe(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireProbe(t, fakeRequesterGetter(t))
	})
	t.Run("p2p", func(t *testing.T) {
		testWireProbe(t, p2pRequesterGetter(t))
	})
}

func TestPairwiseSyncerLimits(t *testing.T) {
	for _, tc := range []struct {
		name               string
		clientTrafficLimit int
		clientMessageLimit int
		serverTrafficLimit int
		serverMessageLimit int
		error              bool
	}{
		{
			name:               "client traffic limit hit",
			clientTrafficLimit: 100,
			error:              true,
		},
		{
			name:               "client message limit hit",
			clientMessageLimit: 10,
			error:              true,
		},
		{
			name:               "server traffic limit hit",
			serverTrafficLimit: 100,
			error:              true,
		},
		{
			name:               "server message limit hit",
			serverMessageLimit: 10,
			error:              true,
		},
		{
			name:               "reasonable limits",
			clientTrafficLimit: 100_000,
			clientMessageLimit: 1000,
			serverTrafficLimit: 100_000,
			serverMessageLimit: 1000,
			error:              false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newHashSyncTester(t, hashSyncTestConfig{
				maxSendRange:    1,
				numTestHashes:   1000,
				minNumSpecificA: 4,
				maxNumSpecificA: 10,
				minNumSpecificB: 4,
				maxNumSpecificB: 10,
			})
			clock := clockwork.NewFakeClockAt(startDate)
			var tr syncTracer
			srvCfg := st.cfg
			srvCfg.TrafficLimit = tc.serverTrafficLimit
			srvCfg.MessageLimit = tc.serverMessageLimit
			cst, ctx := newClientServerTester(
				t, st.setA, p2pRequesterGetter(t),
				srvCfg, &tr, clock)
			logger := zap.NewNop()
			clientCfg := st.cfg
			clientCfg.TrafficLimit = tc.clientTrafficLimit
			clientCfg.MessageLimit = tc.clientMessageLimit
			pss := rangesync.NewPairwiseSetSyncerInternal(
				logger, cst.client, "test", clientCfg, &tr, clock)
			err := pss.Sync(ctx, cst.srvPeerID, st.setB, nil, nil)
			if tc.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
