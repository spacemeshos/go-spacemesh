package hashsync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type incomingRequest struct {
	initialRequest []byte
	stream         io.ReadWriter
}

type fakeRequester struct {
	id            p2p.Peer
	handler       server.StreamHandler
	peers         map[p2p.Peer]*fakeRequester
	reqCh         chan incomingRequest
	bytesSent     uint32
	bytesReceived uint32
}

var _ Requester = &fakeRequester{}

func newFakeRequester(id p2p.Peer, handler server.StreamHandler, peers ...Requester) *fakeRequester {
	fr := &fakeRequester{
		id:      id,
		handler: handler,
		reqCh:   make(chan incomingRequest),
		peers:   make(map[p2p.Peer]*fakeRequester),
	}
	for _, p := range peers {
		pfr := p.(*fakeRequester)
		fr.peers[pfr.id] = pfr
	}
	return fr
}

func (fr *fakeRequester) Run(ctx context.Context) error {
	if fr.handler == nil {
		panic("no handler")
	}
	for {
		var req incomingRequest
		select {
		case <-ctx.Done():
			return nil
		case req = <-fr.reqCh:
		}
		if err := fr.handler(ctx, req.initialRequest, req.stream); err != nil {
			panic("handler error: " + err.Error())
		}
	}
}

func (fr *fakeRequester) request(
	ctx context.Context,
	pid p2p.Peer,
	initialRequest []byte,
	callback server.StreamRequestCallback,
) error {
	p, found := fr.peers[pid]
	if !found {
		return fmt.Errorf("bad peer %q", pid)
	}
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	stream := struct {
		io.Reader
		io.Writer
	}{
		Reader: r,
		Writer: w,
	}
	select {
	case p.reqCh <- incomingRequest{
		initialRequest: initialRequest,
		stream:         stream,
	}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return callback(ctx, stream)
}

func (fr *fakeRequester) StreamRequest(
	ctx context.Context,
	pid p2p.Peer,
	initialRequest []byte,
	callback server.StreamRequestCallback,
	extraProtocols ...string,
) error {
	return fr.request(ctx, pid, initialRequest, callback)
}

type sliceIterator struct {
	s []Ordered
}

var _ Iterator = &sliceIterator{}

func (it *sliceIterator) Equal(other Iterator) bool {
	// not used by wireConduit
	return false
}

func (it *sliceIterator) Key() (Ordered, error) {
	if len(it.s) != 0 {
		return it.s[0], nil
	}
	return nil, nil
}

func (it *sliceIterator) Next() error {
	if len(it.s) != 0 {
		it.s = it.s[1:]
	}
	return nil
}

func (it *sliceIterator) Clone() Iterator {
	return &sliceIterator{s: it.s}
}

type fakeSend struct {
	x, y     Ordered
	count    int
	fp       any
	items    []Ordered
	endRound bool
	done     bool
}

func (fs *fakeSend) send(c Conduit) error {
	switch {
	case fs.endRound:
		return c.SendEndRound()
	case fs.done:
		return c.SendDone()
	case len(fs.items) != 0:
		items := slices.Clone(fs.items)
		return c.SendItems(len(items), 2, &sliceIterator{s: items})
	case fs.x == nil || fs.y == nil:
		return c.SendEmptySet()
	case fs.count == 0:
		return c.SendEmptyRange(fs.x, fs.y)
	case fs.fp != nil:
		return c.SendFingerprint(fs.x, fs.y, fs.fp, fs.count)
	default:
		return c.SendRangeContents(fs.x, fs.y, fs.count)
	}
}

type fakeRound struct {
	name       string
	expectMsgs []SyncMessage
	toSend     []*fakeSend
}

func (r *fakeRound) handleMessages(t *testing.T, c Conduit) error {
	var msgs []SyncMessage
	for {
		msg, err := c.NextMessage()
		if err != nil {
			return fmt.Errorf("NextMessage(): %w", err)
		} else if msg == nil {
			break
		}
		msgs = append(msgs, msg)
		if msg.Type() == MessageTypeDone || msg.Type() == MessageTypeEndRound {
			break
		}
	}
	require.Equal(t, r.expectMsgs, msgs, "messages for round %q", r.name)
	return nil
}

func (r *fakeRound) handleConversation(t *testing.T, c *wireConduit) error {
	if err := r.handleMessages(t, c); err != nil {
		return err
	}
	for _, s := range r.toSend {
		if err := s.send(c); err != nil {
			return err
		}
	}
	return nil
}

func makeTestStreamHandler(t *testing.T, c *wireConduit, rounds []fakeRound) server.StreamHandler {
	cbk := makeTestRequestCallback(t, c, rounds)
	return func(ctx context.Context, initialRequest []byte, stream io.ReadWriter) error {
		t.Logf("init request bytes: %d", len(initialRequest))
		s := struct {
			io.Reader
			io.Writer
		}{
			// prepend the received request to data being read
			Reader: io.MultiReader(bytes.NewBuffer(initialRequest), stream),
			Writer: stream,
		}
		return cbk(ctx, s)
	}
}

func makeTestRequestCallback(t *testing.T, c *wireConduit, rounds []fakeRound) server.StreamRequestCallback {
	return func(ctx context.Context, stream io.ReadWriter) error {
		if c == nil {
			c = &wireConduit{stream: stream}
		} else {
			c.stream = stream
		}
		for _, round := range rounds {
			if err := round.handleConversation(t, c); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestWireConduit(t *testing.T) {
	hs := make([]types.Hash32, 16)
	for n := range hs {
		hs[n] = types.RandomHash()
	}
	fp := types.Hash12(hs[2][:12])
	srvHandler := makeTestStreamHandler(t, nil, []fakeRound{
		{
			name: "server got 1st request",
			expectMsgs: []SyncMessage{
				&FingerprintMessage{
					RangeX:           hs[0],
					RangeY:           hs[1],
					RangeFingerprint: fp,
					NumItems:         4,
				},
				&EndRoundMessage{},
			},
			toSend: []*fakeSend{
				{
					x:     hs[0],
					y:     hs[3],
					count: 2,
				},
				{
					x:     hs[3],
					y:     hs[6],
					count: 2,
				},
				{
					items: []Ordered{hs[4], hs[5], hs[7], hs[8]},
				},
				{
					endRound: true,
				},
			},
		},
		{
			name: "server got 2nd request",
			expectMsgs: []SyncMessage{
				&ItemBatchMessage{
					ContentKeys: []types.Hash32{hs[9], hs[10]},
				},
				&ItemBatchMessage{
					ContentKeys: []types.Hash32{hs[11]},
				},
				&EndRoundMessage{},
			},
			toSend: []*fakeSend{
				{
					done: true,
				},
			},
		},
	})

	srv := newFakeRequester("srv", srvHandler)
	var eg errgroup.Group
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		eg.Wait()
	}()
	eg.Go(func() error {
		return srv.Run(ctx)
	})

	client := newFakeRequester("client", nil, srv)
	var c wireConduit
	initReq, err := c.withInitialRequest(func(c Conduit) error {
		if err := c.SendFingerprint(hs[0], hs[1], fp, 4); err != nil {
			return err
		}
		return c.SendEndRound()
	})
	require.NoError(t, err)
	clientCbk := makeTestRequestCallback(t, &c, []fakeRound{
		{
			name: "client got 1st response",
			expectMsgs: []SyncMessage{
				&RangeContentsMessage{
					RangeX:   hs[0],
					RangeY:   hs[3],
					NumItems: 2,
				},
				&RangeContentsMessage{
					RangeX:   hs[3],
					RangeY:   hs[6],
					NumItems: 2,
				},
				&ItemBatchMessage{
					ContentKeys: []types.Hash32{hs[4], hs[5]},
				},
				&ItemBatchMessage{
					ContentKeys: []types.Hash32{hs[7], hs[8]},
				},
				&EndRoundMessage{},
			},
			toSend: []*fakeSend{
				{
					items: []Ordered{hs[9], hs[10], hs[11]},
				},
				{
					endRound: true,
				},
			},
		},
		{
			name: "client got 2nd response",
			expectMsgs: []SyncMessage{
				&DoneMessage{},
			},
		},
	})
	err = client.StreamRequest(context.Background(), "srv", initReq, clientCbk)
	require.NoError(t, err)
}

type getRequesterFunc func(name string, handler server.StreamHandler, peers ...Requester) (Requester, p2p.Peer)

func withClientServer(
	store ItemStore,
	getRequester getRequesterFunc,
	opts []RangeSetReconcilerOption,
	toCall func(ctx context.Context, client Requester, srvPeerID p2p.Peer),
) {
	srvHandler := func(ctx context.Context, req []byte, stream io.ReadWriter) error {
		pss := NewPairwiseStoreSyncer(nil, opts)
		return pss.Serve(ctx, req, stream, store)
	}
	srv, srvPeerID := getRequester("srv", srvHandler)
	var eg errgroup.Group
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		eg.Wait()
	}()
	eg.Go(func() error {
		return srv.Run(ctx)
	})

	client, _ := getRequester("client", nil, srv)
	toCall(ctx, client, srvPeerID)
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

func testWireSync(t *testing.T, getRequester getRequesterFunc) Requester {
	cfg := xorSyncTestConfig{
		// large test:
		// maxSendRange:    1,
		// numTestHashes:   5000000,
		// minNumSpecificA: 15000,
		// maxNumSpecificA: 20000,
		// minNumSpecificB: 15,
		// maxNumSpecificB: 20,

		// QQQQQ: restore!
		// maxSendRange:    1,
		// numTestHashes:   100000,
		// minNumSpecificA: 4,
		// maxNumSpecificA: 100,
		// minNumSpecificB: 4,
		// maxNumSpecificB: 100,

		maxSendRange:    1,
		numTestHashes:   100,
		minNumSpecificA: 2,
		maxNumSpecificA: 4,
		minNumSpecificB: 2,
		maxNumSpecificB: 4,
	}
	var client Requester
	verifyXORSync(t, cfg, func(storeA, storeB ItemStore, numSpecific int, opts []RangeSetReconcilerOption) bool {
		opts = append(opts, WithRangeSyncLogger(zaptest.NewLogger(t))) // QQQQQ: TBD: rm
		withClientServer(
			storeA, getRequester, opts,
			func(ctx context.Context, client Requester, srvPeerID p2p.Peer) {
				pss := NewPairwiseStoreSyncer(client, opts)
				err := pss.SyncStore(ctx, srvPeerID, storeB, nil, nil)
				require.NoError(t, err)

				if fr, ok := client.(*fakeRequester); ok {
					t.Logf("numSpecific: %d, bytesSent %d, bytesReceived %d",
						numSpecific, fr.bytesSent, fr.bytesReceived)
				}
				t.Logf("bytes read: %d, bytes written: %d", numRead.Load(), numWritten.Load())
			})
		return true
	})
	return client
}

func TestWireSync(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireSync(t, fakeRequesterGetter())
	})
	t.Run("p2p", func(t *testing.T) {
		testWireSync(t, p2pRequesterGetter(t))
	})
}

func testWireProbe(t *testing.T, getRequester getRequesterFunc) Requester {
	cfg := xorSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   10000,
		minNumSpecificA: 130,
		maxNumSpecificA: 130,
		minNumSpecificB: 130,
		maxNumSpecificB: 130,
	}
	var client Requester
	verifyXORSync(t, cfg, func(storeA, storeB ItemStore, numSpecific int, opts []RangeSetReconcilerOption) bool {
		withClientServer(
			storeA, getRequester, opts,
			func(ctx context.Context, client Requester, srvPeerID p2p.Peer) {
				pss := NewPairwiseStoreSyncer(client, opts)
				minA, err := storeA.Min()
				require.NoError(t, err)
				kA, err := minA.Key()
				require.NoError(t, err)
				infoA, err := storeA.GetRangeInfo(nil, kA, kA, -1)
				require.NoError(t, err)
				prA, err := pss.Probe(ctx, srvPeerID, storeB, nil, nil)
				require.NoError(t, err)
				require.Equal(t, infoA.Fingerprint, prA.FP)
				require.Equal(t, infoA.Count, prA.Count)
				require.InDelta(t, 0.98, prA.Sim, 0.05, "sim")

				minA, err = storeA.Min()
				require.NoError(t, err)
				kA, err = minA.Key()
				require.NoError(t, err)
				partInfoA, err := storeA.GetRangeInfo(nil, kA, kA, infoA.Count/2)
				require.NoError(t, err)
				xK, err := partInfoA.Start.Key()
				require.NoError(t, err)
				x := xK.(types.Hash32)
				yK, err := partInfoA.End.Key()
				y := yK.(types.Hash32)
				// partInfoA = storeA.GetRangeInfo(nil, x, y, -1)
				prA, err = pss.Probe(ctx, srvPeerID, storeB, &x, &y)
				require.NoError(t, err)
				require.Equal(t, partInfoA.Fingerprint, prA.FP)
				require.Equal(t, partInfoA.Count, prA.Count)
				require.InDelta(t, 0.98, prA.Sim, 0.1, "sim")
				// QQQQQ: TBD: check prA.Sim and prB.Sim values
			})
		return false
	})
	return client
}

func TestWireProbe(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireProbe(t, fakeRequesterGetter())
	})
	t.Run("p2p", func(t *testing.T) {
		testWireProbe(t, p2pRequesterGetter(t))
	})
}

// TODO: test bounded sync
// TODO: test fail handler
