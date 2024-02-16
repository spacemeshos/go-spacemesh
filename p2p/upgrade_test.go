package p2p

import (
	"sync/atomic"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
)

func TestConnectionsNotifier(t *testing.T) {
	const n = 3
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)
	counter := [n]atomic.Uint32{}
	// we count events - not peers
	for i, host := range mesh.Hosts() {
		_, err := Upgrade(host, WithNodeReporter(func() { counter[i].Add(1) }))
		require.NoError(t, err)
	}

	mesh.ConnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID())
	mesh.ConnectPeers(mesh.Hosts()[2].ID(), mesh.Hosts()[0].ID())
	require.Eventually(t, func() bool {
		return counter[0].Load() >= 2 && counter[1].Load() >= 1 && counter[2].Load() >= 1
	}, time.Second, 10*time.Millisecond)

	mesh.DisconnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID())
	require.Eventually(t, func() bool {
		return counter[0].Load() >= 3 && counter[1].Load() >= 2
	}, time.Second, 10*time.Millisecond)
}
