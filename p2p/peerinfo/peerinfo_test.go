package peerinfo

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestPeerConnStats(t *testing.T) {
	var ps PeerRequestStats
	require.Zero(t, ps.SuccessCount())
	require.Zero(t, ps.FailureCount())
	require.Zero(t, ps.Latency())
	ps.RequestDone(time.Second, true)
	ps.RequestDone(2*time.Second, true)
	ps.RequestDone(3*time.Second, false)
	require.Equal(t, 2, ps.SuccessCount())
	require.Equal(t, 1, ps.FailureCount())
	require.Equal(t, 2*time.Second, ps.Latency())
}

func TestPeerInfoTracker(t *testing.T) {
	mesh, err := mocknet.FullMeshLinked(2)
	require.NoError(t, err)
	clk := clockwork.NewFakeClock()
	pt1 := NewPeerInfoTracker(withClock(clk))
	pt1.Start(mesh.Nets()[0])
	pt2 := NewPeerInfoTracker(withClock(clk))
	pt2.Start(mesh.Nets()[1])
	p1, p2 := mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID()
	c1, err := mesh.ConnectPeers(p1, p2)
	require.NoError(t, err)
	require.Equal(t, p1, c1.LocalPeer())
	require.Equal(t, p2, c1.RemotePeer())
	var c2 network.Conn
	require.Eventually(t, func() bool {
		c2conns := mesh.Nets()[1].ConnsToPeer(p1)
		if len(c2conns) < 1 {
			return false
		}
		c2 = c2conns[0]
		require.Equal(t, p1, c2.RemotePeer())
		require.Equal(t, p2, c2.LocalPeer())
		pi1 := pt1.EnsurePeerInfo(p2)
		pi2 := pt2.EnsurePeerInfo(p1)
		return pi1.Kind(c1) == KindOutbound && pi2.Kind(c2) == KindInbound
	}, 10*time.Second, 10*time.Millisecond)

	pt2.RecordReceived(6000, "foo", p1)
	pt2.RecordSent(3000, "foo", p1)

	pi1 := pt2.EnsurePeerInfo(p1)
	require.Equal(t, int64(6000), pi1.BytesReceived())
	require.Equal(t, int64(3000), pi1.BytesSent())
	require.Equal(t, int64(0), pi1.RecvRate(1))
	require.Equal(t, int64(0), pi1.RecvRate(2))
	require.Equal(t, int64(0), pi1.SendRate(1))
	require.Equal(t, int64(0), pi1.SendRate(2))

	ps := pt2.EnsureProtoStats("foo")
	require.Equal(t, int64(6000), ps.BytesReceived())
	require.Equal(t, int64(3000), ps.BytesSent())
	require.Equal(t, int64(0), ps.RecvRate(1))
	require.Equal(t, int64(0), ps.RecvRate(2))
	require.Equal(t, int64(0), ps.SendRate(1))
	require.Equal(t, int64(0), ps.SendRate(2))

	require.ElementsMatch(t, []protocol.ID{"foo", "__total__"}, pt2.Protocols())

	clk.Advance(bpsInterval1)
	require.Eventually(t, func() bool {
		return pi1.RecvRate(1) != 0 && pi1.SendRate(1) != 0 &&
			ps.RecvRate(1) != 0 && ps.SendRate(1) != 0
	}, 10*time.Second, 100*time.Millisecond)
	require.Equal(t, int64(6000), pi1.BytesReceived())
	require.Equal(t, int64(3000), pi1.BytesSent())
	require.Equal(t, int64(600), pi1.RecvRate(1))
	require.Equal(t, int64(300), pi1.SendRate(1))
	require.Equal(t, int64(0), pi1.RecvRate(2))
	require.Equal(t, int64(0), pi1.SendRate(2))
	for _, proto := range []protocol.ID{"foo", "__total__"} {
		ps := pt2.EnsureProtoStats(proto)
		require.Equal(t, int64(6000), ps.BytesReceived())
		require.Equal(t, int64(3000), ps.BytesSent())
		require.Equal(t, int64(600), ps.RecvRate(1))
		require.Equal(t, int64(300), ps.SendRate(1))
		require.Equal(t, int64(0), ps.RecvRate(2))
		require.Equal(t, int64(0), ps.SendRate(2))
	}

	clk.Advance(bpsInterval2)
	require.Eventually(t, func() bool {
		return pi1.RecvRate(1) == 0 && pi1.SendRate(1) == 0 &&
			ps.RecvRate(1) == 0 && ps.SendRate(1) == 0 &&
			pi1.RecvRate(2) != 0 && pi1.SendRate(2) != 0 &&
			ps.RecvRate(2) != 0 && ps.SendRate(2) != 0
	}, 10*time.Second, 100*time.Millisecond)
	require.Equal(t, int64(6000), pi1.BytesReceived())
	require.Equal(t, int64(3000), pi1.BytesSent())
	require.Equal(t, int64(0), pi1.RecvRate(1))
	require.Equal(t, int64(0), pi1.SendRate(1))
	require.Equal(t, int64(20), pi1.RecvRate(2))
	require.Equal(t, int64(10), pi1.SendRate(2))
	for _, proto := range []protocol.ID{"foo", "__total__"} {
		ps := pt2.EnsureProtoStats(proto)
		require.Equal(t, int64(6000), ps.BytesReceived())
		require.Equal(t, int64(3000), ps.BytesSent())
		require.Equal(t, int64(0), ps.RecvRate(1))
		require.Equal(t, int64(0), ps.SendRate(1))
		require.Equal(t, int64(20), ps.RecvRate(2))
		require.Equal(t, int64(10), ps.SendRate(2))
	}
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
	pt := NewPeerInfoTracker()
	pt.Start(mesh.Nets()[0])
	var mt fakeMetricsTracer
	hpt := NewHolePunchTracer(pt, &mt)
	p1, p2, p3 := mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID(), mesh.Hosts()[2].ID()
	c1, err := mesh.ConnectPeers(p1, p2)
	require.NoError(t, err)
	c2, err := mesh.ConnectPeers(p1, p3)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return pt.EnsurePeerInfo(p2).Kind(c1) == KindOutbound &&
			pt.EnsurePeerInfo(p3).Kind(c2) == KindOutbound
	}, 10*time.Second, 10*time.Millisecond)
	hpt.DirectDialFinished(true)
	hpt.DirectDialFinished(false)
	hpt.HolePunchFinished("receiver", 0, nil, nil, c1)
	hpt.HolePunchFinished("initiator", 0, nil, nil, c2)
	require.Equal(t, 2, mt.dialCount)
	require.Equal(t, 2, mt.holePunchCount)
	require.Equal(t, KindHolePunchInbound, pt.EnsurePeerInfo(p2).Kind(c1))
	require.Equal(t, KindHolePunchOutbound, pt.EnsurePeerInfo(p3).Kind(c2))
}
