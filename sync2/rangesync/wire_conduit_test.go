package rangesync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type incomingRequest struct {
	initialRequest []byte
	stream         io.ReadWriter
}

type fakeRequester struct {
	id      p2p.Peer
	handler server.StreamHandler
	peers   map[p2p.Peer]*fakeRequester
	reqCh   chan incomingRequest
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

type fakeSend struct {
	x, y     types.Ordered
	count    int
	fp       types.Fingerprint
	items    []types.Ordered
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
		return c.SendChunk(slices.Clone(fs.items))
	case fs.x == nil || fs.y == nil:
		return c.SendEmptySet()
	case fs.count == 0:
		return c.SendEmptyRange(fs.x, fs.y)
	case fs.fp != types.EmptyFingerprint():
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
	hs := make([]types.KeyBytes, 16)
	for n := range hs {
		hs[n] = types.RandomKeyBytes(32)
	}
	fp := types.Fingerprint(hs[2][:12])
	srvHandler := makeTestStreamHandler(t, nil, []fakeRound{
		{
			name: "server got 1st request",
			expectMsgs: []SyncMessage{
				&FingerprintMessage{
					RangeX:           KeyBytesToCompact(hs[0]),
					RangeY:           KeyBytesToCompact(hs[1]),
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
					items: []types.Ordered{hs[4], hs[5], hs[7], hs[8]},
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
					ContentKeys: KeyCollection{
						Keys: []types.KeyBytes{hs[9], hs[10], hs[11]},
					},
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
					RangeX:   KeyBytesToCompact(hs[0]),
					RangeY:   KeyBytesToCompact(hs[3]),
					NumItems: 2,
				},
				&RangeContentsMessage{
					RangeX:   KeyBytesToCompact(hs[3]),
					RangeY:   KeyBytesToCompact(hs[6]),
					NumItems: 2,
				},
				&ItemBatchMessage{
					ContentKeys: KeyCollection{
						Keys: []types.KeyBytes{hs[4], hs[5], hs[7], hs[8]},
					},
				},
				&EndRoundMessage{},
			},
			toSend: []*fakeSend{
				{
					items: []types.Ordered{hs[9], hs[10], hs[11]},
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
