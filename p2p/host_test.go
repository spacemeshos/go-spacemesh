package p2p

import (
	"context"
	"fmt"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestPrologue(t *testing.T) {
	cfg1 := DefaultConfig()
	cfg1.DataDir = t.TempDir()
	cfg1.Listen = "/ip4/127.0.0.1/tcp/0"
	cfg2 := DefaultConfig()
	cfg2.DataDir = t.TempDir()
	cfg2.Listen = "/ip4/127.0.0.1/tcp/0"

	h1, err := New(context.Background(), logtest.New(t), cfg1, []byte("red"))
	require.NoError(t, err)
	h2, err := New(context.Background(), logtest.New(t), cfg2, []byte("blue"))
	require.NoError(t, err)
	err = h1.Connect(context.Background(), peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})
	require.ErrorContains(t, err, "failed to negotiate security protocol")
}

func TestLimits(t *testing.T) {
	limits := rcmgr.DefaultLimits
	limits.SystemBaseLimit.ConnsInbound = 2048
	l1 := limits.Scale((64<<30)/4, 1048576/2)
	fmt.Printf("%+v\n", l1)
	// {system:{Streams:18432 StreamsInbound:9216 StreamsOutbound:18432 Conns:1152 ConnsInbound:576 ConnsOutbound:1152 FD:524288 Memory:8724152320} transient:{Streams:2304 StreamsInbound:1152 StreamsOutbound:2304 Conns:320 ConnsInbound:160 ConnsOutbound:320 FD:131072 Memory:1107296256} allowlistedSystem:{Streams:18432 StreamsInbound:9216 StreamsOutbound:18432 Conns:1152 ConnsInbound:576 ConnsOutbound:1152 FD:524288 Memory:8724152320} allowlistedTransient:{Streams:2304 StreamsInbound:1152 StreamsOutbound:2304 Conns:320 ConnsInbound:160 ConnsOutbound:320 FD:131072 Memory:1107296256} serviceDefault:{Streams:20480 StreamsInbound:5120 StreamsOutbound:20480 Conns:0 ConnsInbound:0 ConnsOutbound:0 FD:0 Memory:1140850688} service:map[] servicePeerDefault:{Streams:320 StreamsInbound:160 StreamsOutbound:320 Conns:0 ConnsInbound:0 ConnsOutbound:0 FD:0 Memory:50331648} servicePeer:map[] protocolDefault:{Streams:6144 StreamsInbound:2560 StreamsOutbound:6144 Conns:0 ConnsInbound:0 ConnsOutbound:0 FD:0 Memory:1442840576} protocol:map[] protocolPeerDefault:{Streams:384 StreamsInbound:96 StreamsOutbound:192 Conns:0 ConnsInbound:0 ConnsOutbound:0 FD:0 Memory:16777248} protocolPeer:map[] peerDefault:{Streams:2560 StreamsInbound:1280 StreamsOutbound:2560 Conns:8 ConnsInbound:8 ConnsOutbound:8 FD:8192 Memory:1140850688} peer:map[] conn:{Streams:0 StreamsInbound:0 StreamsOutbound:0 Conns:1 ConnsInbound:1 ConnsOutbound:1 FD:1 Memory:33554432} stream:{Streams:1 StreamsInbound:1 StreamsOutbound:1 Conns:0 ConnsInbound:0 ConnsOutbound:0 FD:0 Memory:16777216}}

}
