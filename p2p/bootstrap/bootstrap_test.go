package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p/core/network"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/bootstrap/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
)

func TestBootstrapEmitEvents(t *testing.T) {
	n := 3
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)
	h := mesh.Hosts()[0]

	emitter, err := h.EventBus().Emitter(new(handshake.EventHandshakeComplete))
	require.NoError(t, err)
	t.Cleanup(func() { _ = emitter.Close() })
	sub, err := h.EventBus().Subscribe(new(EventSpacemeshPeer))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })

	ctrl := gomock.NewController(t)
	discovery := mocks.NewMockDiscovery(ctrl)
	discovery.EXPECT().Bootstrap(gomock.Any()).AnyTimes()

	boot, err := NewBootstrap(
		logtest.New(t),
		BootstrapConfig{TargetOutbound: 5, Timeout: 5 * time.Second},
		h,
		discovery,
	)
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

func TestBootstrapCancelDiscoveryContext(t *testing.T) {
	n := 3
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)
	h := mesh.Hosts()[0]

	emitter, err := h.EventBus().Emitter(new(handshake.EventHandshakeComplete))
	require.NoError(t, err)
	t.Cleanup(func() { _ = emitter.Close() })

	ctrl := gomock.NewController(t)
	discovery := mocks.NewMockDiscovery(ctrl)
	bootch := make(chan context.Context, 1)
	discovery.EXPECT().Bootstrap(gomock.Any()).AnyTimes().Do(func(ctx context.Context) {
		select {
		case bootch <- ctx:
		default:
		}
	})

	boot, err := NewBootstrap(
		logtest.New(t),
		BootstrapConfig{TargetOutbound: n - 1, Timeout: 5 * time.Second},
		h,
		discovery,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = boot.Stop() })

	for _, peer := range mesh.Hosts()[1:] {
		_, err := mesh.ConnectPeers(h.ID(), peer.ID())
		require.NoError(t, err)
		emitter.Emit(handshake.EventHandshakeComplete{
			PID:       peer.ID(),
			Direction: network.DirOutbound,
		})
	}
	timeout := 200 * time.Millisecond
	select {
	case bootctx := <-bootch:
		select {
		case <-bootctx.Done():
			require.Equal(t, bootctx.Err(), context.Canceled)
		case <-time.After(timeout):
			require.FailNow(t, "timed out on waiting for bootctx cancellation")
		}
	case <-time.After(timeout):
		require.FailNow(t, "timed out on waiting for a call to Bootstrap")
	}
}
