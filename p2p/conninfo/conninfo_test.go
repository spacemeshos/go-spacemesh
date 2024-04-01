package conninfo

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestConnInfoTracker(t *testing.T) {
	mesh, err := mocknet.FullMeshLinked(2)
	require.NoError(t, err)
	ct1 := NewConnInfoTracker(mesh.Nets()[0])
	ct2 := NewConnInfoTracker(mesh.Nets()[1])
	c1, err := mesh.ConnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID())
	require.NoError(t, err)
	var c2 network.Conn
	require.Eventually(t, func() bool {
		c2conns := mesh.Nets()[1].ConnsToPeer(mesh.Hosts()[0].ID())
		if len(c2conns) < 1 {
			return false
		}
		c2 = c2conns[0]
		ci1 := ct1.EnsureConnInfo(c1)
		ci2 := ct2.EnsureConnInfo(c2)
		return ci1.Kind() == KindOutbound && ci2.Kind() == KindInbound
	}, 10*time.Second, 10*time.Millisecond)

	mesh.Hosts()[0].SetStreamHandler("test", ct1.WrapStreamHandler(func(s network.Stream) {
		b := make([]byte, 3)
		_, err := io.ReadFull(s, b)
		require.NoError(t, err)
		require.Equal(t, []byte("123"), b)
		_, err = s.Write([]byte("abcde"))
		require.NoError(t, err)
	}))

	s, err := mesh.Hosts()[1].NewStream(context.Background(), mesh.Hosts()[0].ID(), "test")
	cs := ct2.WrapClientStream(s)
	require.NoError(t, err)
	_, err = cs.Write([]byte("123"))
	require.NoError(t, err)
	b := make([]byte, 5)
	_, err = io.ReadFull(cs, b)
	require.NoError(t, err)
	require.Equal(t, []byte("abcde"), b)

	ci1 := ct1.EnsureConnInfo(c1)
	require.Zero(t, ci1.ClientConnStats.BytesSent())
	require.Zero(t, ci1.ClientConnStats.BytesReceived())
	require.Equal(t, 5, ci1.ServerConnStats.BytesSent())
	require.Equal(t, 3, ci1.ServerConnStats.BytesReceived())

	ci2 := ct2.EnsureConnInfo(c2)
	require.Equal(t, 3, ci2.ClientConnStats.BytesSent())
	require.Equal(t, 5, ci2.ClientConnStats.BytesReceived())
	require.Zero(t, ci2.ServerConnStats.BytesSent())
	require.Zero(t, ci2.ServerConnStats.BytesReceived())
}

type fakeMetricsTracer struct {
	dialCount      int
	holePunchCount int
}

var _ holepunch.MetricsTracer = &fakeMetricsTracer{}

func (t *fakeMetricsTracer) DirectDialFinished(success bool) {
	t.dialCount++
}

func (t *fakeMetricsTracer) HolePunchFinished(
	side string,
	attemptNum int,
	theirAddrs, ourAddr []ma.Multiaddr,
	directConn network.ConnMultiaddrs,
) {
	t.holePunchCount++
}

func TestHolePunchTracer(t *testing.T) {
	mesh, err := mocknet.FullMeshLinked(3)
	require.NoError(t, err)
	ct := NewConnInfoTracker(mesh.Nets()[0])
	var mt fakeMetricsTracer
	hpt := NewHolePunchTracer(&mt)
	hpt.SetConnInfo(ct)
	c1, err := mesh.ConnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID())
	require.NoError(t, err)
	c2, err := mesh.ConnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[2].ID())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return ct.EnsureConnInfo(c1).Kind() == KindOutbound &&
			ct.EnsureConnInfo(c2).Kind() == KindOutbound
	}, 10*time.Second, 10*time.Millisecond)
	hpt.DirectDialFinished(true)
	hpt.DirectDialFinished(false)
	hpt.HolePunchFinished("receiver", 0, nil, nil, c1)
	hpt.HolePunchFinished("initiator", 0, nil, nil, c2)
	require.Equal(t, 2, mt.dialCount)
	require.Equal(t, 2, mt.holePunchCount)
	require.Equal(t, KindHolePunchInbound, ct.EnsureConnInfo(c1).Kind())
	require.Equal(t, KindHolePunchOutbound, ct.EnsureConnInfo(c2).Kind())
}
