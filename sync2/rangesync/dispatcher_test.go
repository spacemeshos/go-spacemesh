package rangesync_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func makeFakeDispHandler(n int) rangesync.Handler {
	return func(ctx context.Context, stream io.ReadWriter) error {
		x := rangesync.KeyBytes(bytes.Repeat([]byte{byte(n)}, 32))
		c := rangesync.StartWireConduit(ctx, stream)
		defer c.End()
		s := rangesync.Sender{c}
		s.SendRangeContents(x, x, n)
		s.SendEndRound()
		return nil
	}
}

func TestDispatcher(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	d := rangesync.NewDispatcher(zaptest.NewLogger(t))
	d.Register("a", makeFakeDispHandler(42))
	d.Register("b", makeFakeDispHandler(43))
	d.Register("c", makeFakeDispHandler(44))

	proto := "itest"
	opts := []server.Opt{
		server.WithTimeout(10 * time.Second),
		server.WithLog(zaptest.NewLogger(t)),
	}
	s := d.SetupServer(mesh.Hosts()[0], proto, opts...)
	require.Equal(t, s, d.Server)
	runRequester(t, s)
	srvPeerID := mesh.Hosts()[0].ID()

	c := server.New(mesh.Hosts()[1], proto, d.Dispatch, opts...)
	for _, tt := range []struct {
		name string
		want int
	}{
		{"a", 42},
		{"b", 43},
		{"c", 44},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, c.StreamRequest(
				context.Background(), srvPeerID, []byte(tt.name),
				func(ctx context.Context, stream io.ReadWriter) error {
					c := rangesync.StartWireConduit(ctx, stream)
					defer c.End()
					m, err := c.NextMessage()
					require.NoError(t, err)
					require.Equal(t, rangesync.MessageTypeRangeContents, m.Type())
					exp := rangesync.KeyBytes(bytes.Repeat([]byte{byte(tt.want)}, 32))
					require.Equal(t, exp, m.X())
					require.Equal(t, exp, m.Y())
					require.Equal(t, tt.want, m.Count())
					m, err = c.NextMessage()
					require.NoError(t, err)
					require.Equal(t, rangesync.MessageTypeEndRound, m.Type())
					return nil
				}))
		})
	}
}
