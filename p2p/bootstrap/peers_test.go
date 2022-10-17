package bootstrap

import (
	"context"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
)

func sortPids(pids []peer.ID) []peer.ID {
	sort.Slice(pids, func(i, j int) bool {
		return pids[i] < pids[j]
	})
	return pids
}

func TestPeersAddRemove(t *testing.T) {
	mesh, err := mocknet.WithNPeers(1)
	require.NoError(t, err)

	h := mesh.Hosts()[0]

	emitter, err := h.EventBus().Emitter(new(EventSpacemeshPeer))
	require.NoError(t, err)

	ps := StartPeers(h)
	t.Cleanup(ps.Stop)

	n := 10
	expected := []peer.ID{}
	for i := 0; i < n; i++ {
		pid := peer.ID(strconv.Itoa(i))
		require.NoError(t,
			emitter.Emit(EventSpacemeshPeer{
				PID:           pid,
				Connectedness: network.Connected,
			}))
		expected = append(expected, pid)
	}
	timed, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	t.Cleanup(cancel)
	waited, err := ps.WaitPeers(timed, n)
	require.NoError(t, err)
	require.Equal(t, expected, sortPids(waited))
	require.EqualValues(t, n, ps.PeerCount())
	require.Equal(t, expected, sortPids(ps.GetPeers()))

	for i := 0; i < n/2; i++ {
		require.NoError(t,
			emitter.Emit(EventSpacemeshPeer{
				PID:           peer.ID(strconv.Itoa(i)),
				Connectedness: network.NotConnected,
			}))
	}
	require.Eventually(t, func() bool {
		return int(ps.PeerCount()) == n/2
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, expected[n/2:], sortPids(ps.GetPeers()))
}
