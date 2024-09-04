package rangesync

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type getRequesterFunc func(name string, handler server.StreamHandler, peers ...Requester) (Requester, p2p.Peer)

type clientServerTester struct {
	client            Requester
	srvPeerID         p2p.Peer
	bytesReadValue    atomic.Int64
	bytesWrittenValue atomic.Int64
}

func newClientServerTester(
	t *testing.T,
	set OrderedSet,
	getRequester getRequesterFunc,
	opts []RangeSetReconcilerOption,
) (*clientServerTester, context.Context) {
	var (
		cst clientServerTester
		srv Requester
	)
	srvHandler := func(ctx context.Context, req []byte, stream io.ReadWriter) error {
		pss := NewPairwiseSetSyncer(nil, opts)
		return pss.Serve(ctx, req, wrapStream(&cst, stream), set)
	}
	srv, cst.srvPeerID = getRequester("srv", srvHandler)
	var eg errgroup.Group
	ctx, cancel := context.WithCancel(context.Background())
	eg.Go(func() error {
		return srv.Run(ctx)
	})
	t.Cleanup(func() {
		cancel()
		eg.Wait()
	})

	cst.client, _ = getRequester("client", nil, srv)
	return &cst, ctx
}

func (cst *clientServerTester) bytesRead() int64 {
	return cst.bytesReadValue.Load()
}

func (cst *clientServerTester) bytesWritten() int64 {
	return cst.bytesWrittenValue.Load()
}

type countingStream struct {
	io.ReadWriter
	cst *clientServerTester
}

func (s *countingStream) Read(p []byte) (n int, err error) {
	n, err = s.ReadWriter.Read(p)
	s.cst.bytesReadValue.Add(int64(n))
	return n, err
}

func (s *countingStream) Write(p []byte) (n int, err error) {
	n, err = s.ReadWriter.Write(p)
	s.cst.bytesWrittenValue.Add(int64(n))
	return n, err
}

func wrapStream(cst *clientServerTester, s io.ReadWriter) io.ReadWriter {
	return &countingStream{cst: cst, ReadWriter: s}
}

func fakeRequesterGetter() getRequesterFunc {
	return func(name string, handler server.StreamHandler, peers ...Requester) (Requester, p2p.Peer) {
		pid := p2p.Peer(name)
		return newFakeRequester(pid, handler, peers...), pid
	}
}

func p2pRequesterGetter(t *testing.T) getRequesterFunc {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)
	proto := "itest"
	opts := []server.Opt{
		server.WithRequestSizeLimit(100_000_000),
		server.WithTimeout(10 * time.Second),
		server.WithLog(zaptest.NewLogger(t)),
	}
	return func(name string, handler server.StreamHandler, peers ...Requester) (Requester, p2p.Peer) {
		if len(peers) == 0 {
			return server.New(mesh.Hosts()[0], proto, handler, opts...), mesh.Hosts()[0].ID()
		}
		s := server.New(mesh.Hosts()[1], proto, handler, opts...)
		// TODO: this 'Eventually' is somewhat misplaced
		require.Eventually(t, func() bool {
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
	dumb          bool
	receivedItems int
	sentItems     int
}

var _ Tracer = &syncTracer{}

func (tr *syncTracer) OnDumbSync() {
	tr.dumb = true
}

func (tr *syncTracer) OnRecent(receivedItems, sentItems int) {
	tr.receivedItems += receivedItems
	tr.sentItems += sentItems
}

type fakeRecentSet struct {
	OrderedSet
	timestamps map[string]time.Time
	clock      clockwork.Clock
}

var _ OrderedSet = &fakeRecentSet{}

var startDate = time.Date(2024, 8, 29, 18, 0, 0, 0, time.UTC)

func (frs *fakeRecentSet) registerAll(ctx context.Context) error {
	frs.timestamps = make(map[string]time.Time)
	t := startDate
	items, err := CollectSetItems[types.KeyBytes](ctx, frs.OrderedSet)
	if err != nil {
		return err
	}
	for _, v := range items {
		frs.timestamps[string(v)] = t
		t = t.Add(time.Second)
	}
	return nil
}

func (frs *fakeRecentSet) Add(ctx context.Context, k types.Ordered) error {
	if err := frs.OrderedSet.Add(ctx, k); err != nil {
		return err
	}
	h := k.(types.KeyBytes)
	frs.timestamps[string(h)] = frs.clock.Now()
	return nil
}

func (frs *fakeRecentSet) Recent(ctx context.Context, since time.Time) (types.Seq, int, error) {
	var items []types.KeyBytes
	items, err := CollectSetItems[types.KeyBytes](ctx, frs.OrderedSet)
	if err != nil {
		return nil, 0, err
	}
	for _, k := range items {
		if !frs.timestamps[string(k)].Before(since) {
			items = append(items, k)
		}
	}
	return func(yield func(types.Ordered, error) bool) {
		for _, h := range items {
			if !yield(h, nil) {
				return
			}
		}
	}, len(items), nil
}

func testWireSync(t *testing.T, getRequester getRequesterFunc) {
	for _, tc := range []struct {
		name           string
		cfg            hashSyncTestConfig
		dumb           bool
		opts           []RangeSetReconcilerOption
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
				allowReAdd:      true,
			},
			dumb: false,
			opts: []RangeSetReconcilerOption{
				WithRecentTimeSpan(990 * time.Second),
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
			opts := append(st.opts, WithTracer(&tr), WithClock(clock))
			opts = append(opts, tc.opts...)
			opts = opts[0:len(opts):len(opts)]
			clock.Advance(tc.advance)
			cst, ctx := newClientServerTester(t, setA, getRequester, opts)
			// nr := RmmeNumRead()
			// nw := RmmeNumWritten()
			pss := NewPairwiseSetSyncer(cst.client, opts)
			err := pss.Sync(ctx, cst.srvPeerID, setB, nil, nil)
			require.NoError(t, err)

			t.Logf("numSpecific: %d, bytesSent %d, bytesReceived %d",
				st.numSpecificA+st.numSpecificB,
				cst.bytesRead(), cst.bytesWritten())
			require.Equal(t, tc.dumb, tr.dumb, "dumb sync")
			require.Equal(t, tc.receivedRecent, tr.receivedItems > 0)
			require.Equal(t, tc.sentRecent, tr.sentItems > 0)
			st.verify()
		})
	}
}

func TestWireSync(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireSync(t, fakeRequesterGetter())
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
	cst, ctx := newClientServerTester(t, st.setA, getRequester, st.opts)
	pss := NewPairwiseSetSyncer(cst.client, st.opts)
	itemsA, err := st.setA.Items(ctx)
	require.NoError(t, err)
	kA, err := itemsA.First()
	require.NoError(t, err)
	infoA, err := st.setA.GetRangeInfo(ctx, kA, kA, -1)
	require.NoError(t, err)
	prA, err := pss.Probe(ctx, cst.srvPeerID, st.setB, nil, nil)
	require.NoError(t, err)
	require.Equal(t, infoA.Fingerprint, prA.FP)
	require.Equal(t, infoA.Count, prA.Count)
	require.InDelta(t, 0.98, prA.Sim, 0.05, "sim")

	itemsA, err = st.setA.Items(ctx)
	require.NoError(t, err)
	kA, err = itemsA.First()
	require.NoError(t, err)
	partInfoA, err := st.setA.GetRangeInfo(ctx, kA, kA, infoA.Count/2)
	require.NoError(t, err)
	xK, err := partInfoA.Items.First()
	require.NoError(t, err)
	x := xK.(types.KeyBytes)
	var y types.KeyBytes
	n := partInfoA.Count + 1
	for k, err := range partInfoA.Items {
		if err != nil {
			break
		}
		y = k.(types.KeyBytes)
		n--
		if n == 0 {
			break
		}
	}
	prA, err = pss.Probe(ctx, cst.srvPeerID, st.setB, x, y)
	require.NoError(t, err)
	require.Equal(t, partInfoA.Fingerprint, prA.FP)
	require.Equal(t, partInfoA.Count, prA.Count)
	require.InDelta(t, 0.98, prA.Sim, 0.1, "sim")
}

func TestWireProbe(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireProbe(t, fakeRequesterGetter())
	})
	t.Run("p2p", func(t *testing.T) {
		testWireProbe(t, p2pRequesterGetter(t))
	})
}
