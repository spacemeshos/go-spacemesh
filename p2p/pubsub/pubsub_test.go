package pubsub

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestGossip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := 5
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)

	topic := "test"
	pubsubs := make([]PubSub, 0, n)
	count := n * n
	var received atomic.Int32

	logger := zaptest.NewLogger(t)
	for i, h := range mesh.Hosts() {
		logger := logger.Named(fmt.Sprintf("host-%d", i))
		ps, err := New(ctx, logger, h, Config{Flood: true, IsBootnode: true, QueueSize: 1000, Throttle: 1000})
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		ps.Register(topic, func(ctx context.Context, pid peer.ID, msg []byte) error {
			received.Add(1)
			return nil
		})
	}
	// connect after initializing gossip sub protocol for every peer. otherwise stream initialize
	// maybe fail if other side wasn't able to initialize gossipsub on time.
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(topic)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 10*time.Second, 100*time.Millisecond)
	for i, ps := range pubsubs {
		require.NoError(t, ps.Publish(ctx, topic, []byte(mesh.Hosts()[i].ID())))
	}
	require.Eventually(t, func() bool { return received.Load() == int32(count) }, 5*time.Second, 10*time.Millisecond)
}
