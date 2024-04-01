package conninfo

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
)

func TestPeerConnStats(t *testing.T) {
	var ps PeerConnectionStats
	require.Zero(t, ps.SuccessCount())
	require.Zero(t, ps.FailureCount())
	require.Zero(t, ps.Latency())
	require.Zero(t, ps.BytesSent())
	require.Zero(t, ps.BytesReceived())
	ps.RequestDone(time.Second, true)
	ps.RequestDone(2*time.Second, true)
	ps.RequestDone(3*time.Second, false)
	ps.recordSent(42)
	ps.recordReceived(4242)
	require.Equal(t, 2, ps.SuccessCount())
	require.Equal(t, 1, ps.FailureCount())
	require.Equal(t, 2*time.Second, ps.Latency())
	require.Equal(t, 42, ps.BytesSent())
	require.Equal(t, 4242, ps.BytesReceived())
}

type fakeConnInfo struct {
	Info
}

var _ ConnInfo = &fakeConnInfo{}

func (fci *fakeConnInfo) EnsureConnInfo(c network.Conn) *Info {
	return &fci.Info
}

func TestCountingStream(t *testing.T) {
	var fci fakeConnInfo
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	mesh.Hosts()[0].SetStreamHandler("test", func(s network.Stream) {
		cs := NewCountingStream(&fci, s, true)
		b := make([]byte, 3)
		_, err := io.ReadFull(cs, b)
		require.NoError(t, err)
		require.Equal(t, []byte("123"), b)
		_, err = cs.Write([]byte("abcde"))
		require.NoError(t, err)
	})

	s, err := mesh.Hosts()[1].NewStream(context.Background(), mesh.Hosts()[0].ID(), "test")
	cs := NewCountingStream(&fci, s, false)
	require.NoError(t, err)
	_, err = cs.Write([]byte("123"))
	require.NoError(t, err)
	b := make([]byte, 5)
	_, err = io.ReadFull(cs, b)
	require.NoError(t, err)
	require.Equal(t, []byte("abcde"), b)

	require.Equal(t, 3, fci.ClientConnStats.BytesSent())
	require.Equal(t, 5, fci.ClientConnStats.BytesReceived())
	require.Equal(t, 5, fci.ServerConnStats.BytesSent())
	require.Equal(t, 3, fci.ServerConnStats.BytesReceived())
}
