package hashsync

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type fakeMessage struct {
	data  []byte
	error string
}

type fakeInteractor struct {
	fr     *fakeRequester
	ctx    context.Context
	sendCh chan fakeMessage
	recvCh chan fakeMessage
}

func (i *fakeInteractor) Send(data []byte) error {
	// fmt.Fprintf(os.Stderr, "%p: send %q\n", i, data)
	select {
	case i.sendCh <- fakeMessage{data: data}:
		atomic.AddUint32(&i.fr.bytesSent, uint32(len(data)))
		return nil
	case <-i.ctx.Done():
		return i.ctx.Err()
	}
}

func (i *fakeInteractor) SendError(err error) error {
	// fmt.Fprintf(os.Stderr, "%p: send error %q\n", i, err)
	select {
	case i.sendCh <- fakeMessage{error: err.Error()}:
		atomic.AddUint32(&i.fr.bytesSent, uint32(len(err.Error())))
		return nil
	case <-i.ctx.Done():
		return i.ctx.Err()
	}
}

func (i *fakeInteractor) Receive() ([]byte, error) {
	// fmt.Fprintf(os.Stderr, "%p: receive\n", i)
	var m fakeMessage
	select {
	case m = <-i.recvCh:
	case <-i.ctx.Done():
		return nil, i.ctx.Err()
	}
	// fmt.Fprintf(os.Stderr, "%p: received %#v\n", i, m)
	if m.error != "" {
		atomic.AddUint32(&i.fr.bytesReceived, uint32(len(m.error)))
		return nil, fmt.Errorf("%w: %s", server.RemoteError, m.error)
	}
	atomic.AddUint32(&i.fr.bytesReceived, uint32(len(m.data)))
	return m.data, nil
}

type incomingRequest struct {
	sendCh chan fakeMessage
	recvCh chan fakeMessage
}

var _ server.Interactor = &fakeInteractor{}

type fakeRequester struct {
	id            p2p.Peer
	handler       server.ServerHandler
	peers         map[p2p.Peer]*fakeRequester
	reqCh         chan incomingRequest
	bytesSent     uint32
	bytesReceived uint32
}

var _ requester = &fakeRequester{}

func newFakeRequester(id p2p.Peer, handler server.ServerHandler, peers ...requester) *fakeRequester {
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
		i := &fakeInteractor{
			fr:     fr,
			ctx:    ctx,
			sendCh: req.sendCh,
			recvCh: req.recvCh,
		}
		fr.handler.Handle(ctx, i)
	}
}

func (fr *fakeRequester) request(
	ctx context.Context,
	pid p2p.Peer,
	initialRequest []byte,
	handler server.InteractiveHandler,
) error {
	p, found := fr.peers[pid]
	if !found {
		return fmt.Errorf("bad peer %q", pid)
	}
	i := &fakeInteractor{
		fr:     fr,
		ctx:    ctx,
		sendCh: make(chan fakeMessage, 1),
		recvCh: make(chan fakeMessage),
	}
	i.sendCh <- fakeMessage{data: initialRequest}
	select {
	case p.reqCh <- incomingRequest{
		sendCh: i.recvCh,
		recvCh: i.sendCh,
	}:
	case <-ctx.Done():
		return ctx.Err()
	}
	_, err := handler(ctx, i)
	return err
}

func (fr *fakeRequester) InteractiveRequest(
	ctx context.Context,
	pid p2p.Peer,
	initialRequest []byte,
	handler server.InteractiveHandler,
	failure func(error),
) error {
	go func() {
		err := fr.request(ctx, pid, initialRequest, handler)
		if err != nil {
			failure(err)
		}
	}()
	return nil
}

type sliceIterator struct {
	s []Ordered
}

var _ Iterator = &sliceIterator{}

func (it *sliceIterator) Equal(other Iterator) bool {
	// not used by wireConduit
	return false
}

func (it *sliceIterator) Key() Ordered {
	if len(it.s) != 0 {
		return it.s[0]
	}
	return nil
}

func (it *sliceIterator) Value() any {
	k := it.Key()
	if k == nil {
		return nil
	}
	return mkFakeValue(k.(types.Hash32))
}

func (it *sliceIterator) Next() {
	if len(it.s) != 0 {
		it.s = it.s[1:]
	}
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
	// fmt.Fprintf(os.Stderr, "fakeRound %q: handleMessages\n", r.name)
	var msgs []SyncMessage
	for {
		msg, err := c.NextMessage()
		if err != nil {
			// fmt.Fprintf(os.Stderr, "fakeRound %q: error getting message: %v\n", r.name, err)
			return fmt.Errorf("NextMessage(): %w", err)
		} else if msg == nil {
			// fmt.Fprintf(os.Stderr, "fakeRound %q: consumed all messages\n", r.name)
			break
		}
		// fmt.Fprintf(os.Stderr, "fakeRound %q: got message %#v\n", r.name, msg)
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

func makeTestHandler(t *testing.T, c *wireConduit, newValue NewValueFunc, done chan struct{}, rounds []fakeRound) server.InteractiveHandler {
	return func(ctx context.Context, i server.Interactor) (time.Duration, error) {
		defer func() {
			if done != nil {
				close(done)
			}
		}()
		if c == nil {
			c = &wireConduit{i: i, newValue: newValue}
		} else {
			c.i = i
		}
		for _, round := range rounds {
			if err := round.handleConversation(t, c); err != nil {
				done = nil
				return 0, err
			}
		}
		return 0, nil
	}
}

func TestWireConduit(t *testing.T) {
	hs := make([]types.Hash32, 16)
	for n := range hs {
		hs[n] = types.RandomHash()
	}
	fp := types.Hash12(hs[2][:12])
	srvHandler := makeTestHandler(t, nil, func() any { return new(fakeValue) }, nil, []fakeRound{
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
				&decodedItemBatchMessage{
					ContentKeys:   []types.Hash32{hs[9], hs[10]},
					ContentValues: []any{mkFakeValue(hs[9]), mkFakeValue(hs[10])},
				},
				&decodedItemBatchMessage{
					ContentKeys:   []types.Hash32{hs[11]},
					ContentValues: []any{mkFakeValue(hs[11])},
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
	c.newValue = func() any { return new(fakeValue) }
	initReq, err := c.withInitialRequest(func(c Conduit) error {
		if err := c.SendFingerprint(hs[0], hs[1], fp, 4); err != nil {
			return err
		}
		return c.SendEndRound()
	})
	require.NoError(t, err)
	done := make(chan struct{})
	clientHandler := makeTestHandler(t, &c, c.newValue, done, []fakeRound{
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
				&decodedItemBatchMessage{
					ContentKeys:   []types.Hash32{hs[4], hs[5]},
					ContentValues: []any{mkFakeValue(hs[4]), mkFakeValue(hs[5])},
				},
				&decodedItemBatchMessage{
					ContentKeys:   []types.Hash32{hs[7], hs[8]},
					ContentValues: []any{mkFakeValue(hs[7]), mkFakeValue(hs[8])},
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
	err = client.InteractiveRequest(context.Background(), "srv", initReq, clientHandler, func(err error) {
		t.Errorf("fail handler called: %v", err)
		close(done)
	})
	require.NoError(t, err)
	<-done
}

type getRequesterFunc func(name string, handler server.InteractiveHandler, peers ...requester) (requester, p2p.Peer)

func withClientServer(
	storeA, storeB ItemStore,
	getRequester getRequesterFunc,
	opts []Option,
	toCall func(ctx context.Context, client requester, srvPeerID p2p.Peer),
) {
	srvHandler := MakeServerHandler(storeA, opts...)
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

func fakeRequesterGetter(t *testing.T) getRequesterFunc {
	return func(name string, handler server.InteractiveHandler, peers ...requester) (requester, p2p.Peer) {
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
		server.WithLog(logtest.New(t)),
	}
	return func(name string, handler server.InteractiveHandler, peers ...requester) (requester, p2p.Peer) {
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

func testWireSync(t *testing.T, getRequester getRequesterFunc) requester {
	cfg := xorSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   100000,
		minNumSpecificA: 4,
		maxNumSpecificA: 100,
		minNumSpecificB: 4,
		maxNumSpecificB: 100,
	}
	var client requester
	verifyXORSync(t, cfg, func(storeA, storeB ItemStore, numSpecific int, opts []Option) bool {
		withClientServer(
			storeA, storeB, getRequester, opts,
			func(ctx context.Context, client requester, srvPeerID p2p.Peer) {
				err := SyncStore(ctx, client, srvPeerID, storeB, opts...)
				require.NoError(t, err)

				if fr, ok := client.(*fakeRequester); ok {
					t.Logf("numSpecific: %d, bytesSent %d, bytesReceived %d",
						numSpecific, fr.bytesSent, fr.bytesReceived)
				}
			})
		return true
	})
	return client
}

func TestWireSync(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireSync(t, fakeRequesterGetter(t))
	})
	t.Run("p2p", func(t *testing.T) {
		testWireSync(t, p2pRequesterGetter(t))
	})
}

func testWireProbe(t *testing.T, getRequester getRequesterFunc) requester {
	cfg := xorSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   32,
		minNumSpecificA: 4,
		maxNumSpecificA: 4,
		minNumSpecificB: 4,
		maxNumSpecificB: 4,
	}
	var client requester
	verifyXORSync(t, cfg, func(storeA, storeB ItemStore, numSpecific int, opts []Option) bool {
		withClientServer(
			storeA, storeB, getRequester, opts,
			func(ctx context.Context, client requester, srvPeerID p2p.Peer) {
				minA := storeA.Min().Key()
				infoA := storeA.GetRangeInfo(nil, minA, minA, -1)
				fpA, countA, err := Probe(ctx, client, srvPeerID, opts...)
				require.NoError(t, err)
				require.Equal(t, infoA.Fingerprint, fpA)
				require.Equal(t, infoA.Count, countA)

				minA = storeA.Min().Key()
				partInfoA := storeA.GetRangeInfo(nil, minA, minA, infoA.Count/2)
				x := partInfoA.Start.Key().(types.Hash32)
				y := partInfoA.End.Key().(types.Hash32)
				// partInfoA = storeA.GetRangeInfo(nil, x, y, -1)
				fpA, countA, err = BoundedProbe(ctx, client, srvPeerID, x, y, opts...)
				require.NoError(t, err)
				require.Equal(t, partInfoA.Fingerprint, fpA)
				require.Equal(t, partInfoA.Count, countA)
			})
		return false
	})
	return client
}

func TestWireProbe(t *testing.T) {
	t.Run("fake requester", func(t *testing.T) {
		testWireProbe(t, fakeRequesterGetter(t))
	})
	t.Run("p2p", func(t *testing.T) {
		testWireProbe(t, p2pRequesterGetter(t))
	})
}

// TODO: test bounded sync
// TODO: test fail handler
