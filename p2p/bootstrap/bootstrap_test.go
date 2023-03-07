package bootstrap

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
)

func TestBootstrapEmitEvents(t *testing.T) {
	n := 3
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)
	h := mesh.Hosts()[0]

	emitter, err := h.EventBus().Emitter(new(handshake.EventHandshakeComplete))
	require.NoError(t, err)
	t.Cleanup(func() { emitter.Close() })
	sub, err := h.EventBus().Subscribe(new(EventSpacemeshPeer))
	require.NoError(t, err)
	t.Cleanup(func() { sub.Close() })

	boot, err := NewBootstrap(logtest.New(t), h)
	require.NoError(t, err)
	t.Cleanup(func() { _ = boot.Stop() })

	for _, peer := range mesh.Hosts()[1:] {
		_, err := mesh.ConnectPeers(h.ID(), peer.ID())
		require.NoError(t, err)
		emitter.Emit(handshake.EventHandshakeComplete{
			PID:       peer.ID(),
			Direction: network.DirInbound,
		})
	}
	i := 0
	for evt := range sub.Out() {
		ev, ok := evt.(EventSpacemeshPeer)
		require.True(t, ok, "not an EventSpacemeshPeer")
		require.Equal(t, ev.Connectedness, network.Connected)
		i++
		if i == n-1 {
			break
		}
	}
	for _, peer := range mesh.Hosts()[1:] {
		require.NoError(t, h.Network().ClosePeer(peer.ID()))
		err := mesh.DisconnectPeers(h.ID(), peer.ID())
		require.NoError(t, err)
	}
	i = 0
	for evt := range sub.Out() {
		ev, ok := evt.(EventSpacemeshPeer)
		require.True(t, ok, "not an EventSpacemeshPeer")
		require.Equal(t, ev.Connectedness, network.NotConnected)
		i++
		if i == n-1 {
			break
		}
	}
}
