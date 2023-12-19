package node

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

func TestPeerInfoApi(t *testing.T) {
	cfg := config.DefaultTestConfig()
	cfg.Genesis.Accounts = nil
	cfg.P2P.DisableNatPort = true
	cfg.P2P.Listen = p2p.MustParseAddresses("/ip4/127.0.0.1/tcp/0")

	cfg.API.PublicListener = "0.0.0.0:0"
	cfg.API.PrivateServices = nil
	cfg.API.PublicServices = []string{grpcserver.Admin}
	l := logtest.New(t)
	networkSize := 3
	network := NewTestNetwork(t, cfg, l, networkSize)
	infos := make([][]*pb.PeerInfo, networkSize)
	for i, app := range network {
		adminapi := pb.NewAdminServiceClient(app.Conn)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		streamClient, err := adminapi.PeerInfoStream(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		for {
			info, err := streamClient.Recv()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			infos[i] = append(infos[i], info)
		}
	}

	for i, app := range network {
		for j, innerApp := range network {
			if j == i {
				continue
			}
			peers := infos[i]
			require.Len(t, peers, networkSize-1, "expecting each node to have connections to all other nodes")
			peer := getPeerInfo(peers, innerApp.host.ID())
			require.NotNil(t, peer, "info is missing connection to %v")
			require.Len(t, peer.Connections, 1, "expecting only 1 connection to each peer")
			require.Equal(t, innerApp.host.Addrs()[0].String(), peer.Connections[0].Address, "connection address should match address of peer")
			require.Greater(t, peer.Connections[0].Uptime.AsDuration(), time.Duration(0), "uptime should be set")
			outbound := peer.Connections[0].Outbound

			// Check that outbound matches with the other side of the connection
			otherSide := getPeerInfo(infos[j], app.host.ID())
			require.NotNil(t, peer, "one side missing peer connection")
			require.Len(t, otherSide.Connections, 1, "expecting only 1 connection to each peer")
			require.Equal(t, outbound, !otherSide.Connections[0].Outbound, "expecting pairwise connections to agree on outbound direction")
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
