package node

import (
	"context"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestPeerInfoApi(t *testing.T) {
	cfg := config.DefaultTestConfig()
	cfg.P2P.DisableNatPort = true
	cfg.P2P.Listen = "/ip4/127.0.0.1/tcp/0"

	cfg.API.PublicListener = "0.0.0.0:0"
	cfg.API.PrivateServices = nil
	cfg.API.PublicServices = []string{grpcserver.Admin}
	l := logtest.New(t)
	networkSize := 3
	network := NewTestNetwork(t, cfg, l, networkSize)
	var infos []*pb.PeerInfoResponse
	for _, app := range network {
		adminapi := pb.NewAdminServiceClient(app.Conn)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		info, err := adminapi.PeerInfo(ctx, &empty.Empty{})
		require.NoError(t, err)
		infos = append(infos, info)
	}

	for i, app := range network {
		for j, innerApp := range network {
			if j == i {
				continue
			}
			info := infos[i]
			require.Len(t, info.Peers, 2, "expecting each node to have connections to all other nodes")
			peer := getPeerInfo(info.Peers, innerApp.host.ID())
			require.NotNil(t, peer, "info is missing connection to %v")
			require.Len(t, peer.Connections, 1, "expecting only 1 connection to each peer")
			require.Equal(t, innerApp.host.Addrs()[0].String(), peer.Connections[0].Address, "connection address should match address of peer")
			outbound := peer.Connections[0].Outbound
			// Check that outbound matches with the other side of the connection

			otherSide := getPeerInfo(infos[j].Peers, app.host.ID())
			require.NotNil(t, peer, "one side missing peer connection")
			require.Len(t, otherSide.Connections, 1, "expecting only 1 connection to each peer")
			require.Equal(t, outbound, !otherSide.Connections[0].Outbound)
		}
	}
}

func getPeerInfo(peers []*pb.PeerInfo, id peer.ID) *pb.PeerInfo {
	str := id.String()
	for _, p := range peers {
		if str == p.Id {
			return p
		}
	}
	return nil
}
