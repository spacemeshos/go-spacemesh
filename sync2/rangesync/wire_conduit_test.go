package rangesync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type pipeStream struct {
	io.ReadCloser
	io.WriteCloser
}

func (ps *pipeStream) Close() error {
	return errors.Join(ps.ReadCloser.Close(), ps.WriteCloser.Close())
}

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

func (fr *fakeRequester) StreamRequest(
	ctx context.Context,
	pid p2p.Peer,
	initialRequest []byte,
	callback server.StreamRequestCallback,
	extraProtocols ...string,
) error {
	p, found := fr.peers[pid]
	if !found {
		return fmt.Errorf("bad peer %q", pid)
	}
	rClient, wServer := io.Pipe()
	rServer, wClient := io.Pipe()
	for _, s := range []io.Closer{rClient, wClient, rServer, wServer} {
		defer s.Close()
	}
	clientStream := &pipeStream{ReadCloser: rClient, WriteCloser: wClient}
	serverStream := &pipeStream{ReadCloser: rServer, WriteCloser: wServer}
	select {
	case p.reqCh <- incomingRequest{
		initialRequest: initialRequest,
		stream:         serverStream,
	}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return callback(ctx, clientStream)
}

func runRequester(t *testing.T, r Requester) context.Context {
	var eg errgroup.Group
	ctx, cancel := context.WithCancel(context.Background())
	eg.Go(func() error {
		return r.Run(ctx)
	})
	t.Cleanup(func() {
		cancel()
		eg.Wait()
	})
	return ctx
}

func getMsgs(t *testing.T, c Conduit, n int) []SyncMessage {
	msgs := make([]SyncMessage, n)
	for i := 0; i < n; i++ {
		var err error
		msgs[i], err = c.NextMessage()
		require.NoError(t, err)
	}
	return msgs
}

func TestWireConduit(t *testing.T) {
	hs := make([]types.KeyBytes, 16)
	for n := range hs {
		hs[n] = types.RandomKeyBytes(32)
	}
	fp := types.Fingerprint(hs[2][:12])
	srv := newFakeRequester(
		"srv",
		func(ctx context.Context, initialRequest []byte, stream io.ReadWriter) error {
			require.Equal(t, []byte("hello"), initialRequest)
			c := startWireConduit(ctx, stream)
			defer c.stop()
			s := sender{c}
			require.Equal(t, []SyncMessage{
				&FingerprintMessage{
					RangeX:           chash(hs[0]),
					RangeY:           chash(hs[1]),
					RangeFingerprint: fp,
					NumItems:         4,
				},
				&EndRoundMessage{},
			}, getMsgs(t, c, 2))
			require.NoError(t, s.sendRangeContents(hs[0], hs[3], 2))
			require.NoError(t, s.sendRangeContents(hs[3], hs[6], 2))
			require.NoError(t, s.sendChunk([]types.KeyBytes{hs[4], hs[5], hs[7], hs[8]}))
			require.NoError(t, s.sendEndRound())
			require.Equal(t, []SyncMessage{
				&ItemBatchMessage{
					ContentKeys: KeyCollection{
						Keys: []types.KeyBytes{hs[9], hs[10], hs[11]},
					},
				},
				&EndRoundMessage{},
			}, getMsgs(t, c, 2))
			require.NoError(t, s.sendDone())
			c.end()
			return nil
		})

	runRequester(t, srv)

	client := newFakeRequester("client", nil, srv)
	require.NoError(t, client.StreamRequest(context.Background(), "srv", []byte("hello"),
		func(ctx context.Context, stream io.ReadWriter) error {
			c := startWireConduit(ctx, stream)
			defer c.stop()
			s := sender{c}
			require.NoError(t, s.sendFingerprint(hs[0], hs[1], fp, 4))
			require.NoError(t, s.sendEndRound())
			require.Equal(t, []SyncMessage{
				&RangeContentsMessage{
					RangeX:   chash(hs[0]),
					RangeY:   chash(hs[3]),
					NumItems: 2,
				},
				&RangeContentsMessage{
					RangeX:   chash(hs[3]),
					RangeY:   chash(hs[6]),
					NumItems: 2,
				},
				&ItemBatchMessage{
					ContentKeys: KeyCollection{
						Keys: []types.KeyBytes{hs[4], hs[5], hs[7], hs[8]},
					},
				},
				&EndRoundMessage{},
			}, getMsgs(t, c, 4))
			require.NoError(t, s.sendChunk([]types.KeyBytes{hs[9], hs[10], hs[11]}))
			require.NoError(t, s.sendEndRound())
			require.Equal(t, []SyncMessage{
				&DoneMessage{},
			}, getMsgs(t, c, 1))
			c.end()
			return nil
		}))
}

func TestWireConduit_Limits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  []ConduitOption
		error bool
	}{
		{
			name:  "message limit hit",
			opts:  []ConduitOption{WithMessageLimit(10)},
			error: true,
		},
		{
			name:  "traffic limit hit",
			opts:  []ConduitOption{WithTrafficLimit(100)},
			error: true,
		},
		{
			name:  "limits not hit",
			opts:  []ConduitOption{WithMessageLimit(1000), WithTrafficLimit(10000)},
			error: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errCh := make(chan error)
			srv := newFakeRequester(
				"srv",
				func(ctx context.Context, initialRequest []byte, stream io.ReadWriter) error {
					c := startWireConduit(ctx, stream, tc.opts...)
					defer c.stop()
					for range 11 {
						msg, err := c.NextMessage()
						if err != nil {
							errCh <- err
							return nil
						}
						if msg == nil {
							break
						}
					}
					errCh <- nil
					s := sender{c}
					return s.sendDone()
				})

			runRequester(t, srv)

			client := newFakeRequester("client", nil, srv)
			var eg errgroup.Group
			ctx, cancel := context.WithCancel(context.Background())
			defer func() {
				cancel()
				eg.Wait()
			}()
			eg.Go(func() error {
				client.StreamRequest(ctx, "srv", []byte("hello"),
					func(ctx context.Context, stream io.ReadWriter) error {
						c := startWireConduit(ctx, stream)
						defer c.stop()
						s := sender{c}
						for i := 0; i < 11; i++ {
							s.sendFingerprint(
								types.RandomKeyBytes(32), types.RandomKeyBytes(32),
								types.Fingerprint{}, 1)
						}
						c.NextMessage()
						return nil
					})
				return nil
			})

			if tc.error {
				require.ErrorIs(t, <-errCh, ErrLimitExceeded)
			} else {
				require.NoError(t, <-errCh)
			}
		})
	}
}

func TestWireConduit_StopSend(t *testing.T) {
	started := make(chan struct{})
	srv := newFakeRequester(
		"srv",
		func(ctx context.Context, initialRequest []byte, stream io.ReadWriter) error {
			close(started)
			// This will hang
			<-ctx.Done()
			return nil
		})

	runRequester(t, srv)

	client := newFakeRequester("client", nil, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client.StreamRequest(ctx, "srv", []byte("hello"),
		func(ctx context.Context, stream io.ReadWriter) error {
			c := startWireConduit(ctx, stream)
			s := sender{c}
			// The actual message is enqueued but not sent
			s.sendDone()
			select {
			case <-ctx.Done():
			case <-started:
			}
			c.stop() // stop the sender and wait for it to terminate
			return nil
		})
	require.NoError(t, ctx.Err(), "the context should not be canceled")
}
