package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

func BenchmarkAddPeer(b *testing.B) {
	collector := metrics.NewGoSIPCollector()
	for i := 0; i < b.N; i++ {
		collector.AddPeer("peer", "addr")
	}
}

func BenchmarkRemovePeer(b *testing.B) {
	collector := metrics.NewGoSIPCollector()
	for i := 0; i < b.N; i++ {
		collector.RemovePeer("peer")
	}
}

func TestGoSIPCollector_PeersCounter(t *testing.T) {
	t.Run("increment", func(t *testing.T) {
		t.Parallel()
		collector := metrics.NewGoSIPCollector()
		collector.AddPeer("peer_id_1", "protocol_1")
		collector.AddPeer("peer_id_2", "protocol_1")
		collector.AddPeer("peer_id_3", "protocol_1")
		collector.AddPeer("peer_id_1", "protocol_2")
		collector.AddPeer("peer_id_1", "protocol_2")
		collector.AddPeer("peer_id_1", "protocol_2")

		data := collector.GetStat()
		require.Equal(t, 3, data.TotalPeers, "TotalPeers should be 3")
		require.Equal(t, 3, data.PeersPerProtocol["protocol_1"], "PeersPerProtocol `protocol_1` should be 3")
		require.Equal(t, 1, data.PeersPerProtocol["protocol_2"], "PeersPerProtocol `protocol_2` should be 1")
	})
	t.Run("decrement", func(t *testing.T) {
		t.Parallel()
		collector := metrics.NewGoSIPCollector()
		collector.AddPeer("peer_id_1", "protocol_1")
		collector.AddPeer("peer_id_2", "protocol_1")
		collector.AddPeer("peer_id_3", "protocol_1")
		collector.RemovePeer("peer_id_1")
		collector.RemovePeer("peer_id_1")
		collector.RemovePeer("peer_id_1")

		data := collector.GetStat()
		require.Equal(t, 2, data.TotalPeers, "TotalPeers should be 2")
		require.Equal(t, 2, data.PeersPerProtocol["protocol_1"], "PeersPerProtocol `protocol_1` should be 2")
	})
}
