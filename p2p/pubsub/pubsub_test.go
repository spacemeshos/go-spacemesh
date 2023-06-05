package pubsub

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestGossip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	n := 10
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)
	topic := "test"
	pubsubs := []*PubSub{}
	count := n * n
	received := make(chan []byte, count)

	for _, h := range mesh.Hosts() {
		h := h
		ps, err := New(ctx, logtest.New(t), h, Config{Flood: true, IsBootnode: true})
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		ps.Register(topic, func(ctx context.Context, pid peer.ID, msg []byte) error {
			received <- msg
			return nil
		})
	}
	// connect after initializng gossip sub protocol for every peer. otherwise stream initialize
	// maybe fail if other side wasn't able to initialize gossipsub on time.
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(topic)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	for i, ps := range pubsubs {
		require.NoError(t, ps.Publish(ctx, topic, []byte(mesh.Hosts()[i].ID())))
	}
	require.Eventually(t, func() bool { return len(received) == count }, 5*time.Second, 10*time.Millisecond)
}
