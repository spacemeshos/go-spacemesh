package metrics

import (
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	prometheusMetrics "github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	incoming = "incoming"
	outgoing = "outgoing"
	// subsystem shared by all metrics exposed by this package.
	subsystem = "p2p"
)

var (
	totalIn             = prometheusMetrics.NewCounter("total_send", subsystem, "Total bytes sent", nil)
	totalOut            = prometheusMetrics.NewCounter("total_recv", subsystem, "Total bytes received", nil)
	messagesPerProtocol = prometheusMetrics.NewCounter(
		"messages_per_protocol",
		subsystem,
		"Number of messages sent per protocol",
		[]string{"protocol", "direction"},
	)
	trafficPerProtocol = prometheusMetrics.NewCounter(
		"traffic_per_protocol",
		subsystem,
		"Traffic sent per protocol",
		[]string{"protocol", "direction"},
	)
)

// BandwidthCollector implement metrics.Reporter
// that keeps track of the number of messages sent and received per protocol.
type BandwidthCollector struct{}

// NewBandwidthCollector creates a new BandwidthCollector.
func NewBandwidthCollector() *BandwidthCollector {
	return &BandwidthCollector{}
}

// LogSentMessageStream logs the message node sent to the peer.
func (b *BandwidthCollector) LogSentMessageStream(size int64, proto protocol.ID, p peer.ID) {
	totalOut.WithLabelValues().Add(float64(size))
	trafficPerProtocol.WithLabelValues(string(proto), outgoing).Add(float64(size))
	messagesPerProtocol.WithLabelValues(string(proto), outgoing).Inc()
}

// LogRecvMessageStream logs the message that node received from the peer.
func (b *BandwidthCollector) LogRecvMessageStream(size int64, proto protocol.ID, p peer.ID) {
	totalIn.WithLabelValues().Add(float64(size))
	trafficPerProtocol.WithLabelValues(string(proto), incoming).Add(float64(size))
	messagesPerProtocol.WithLabelValues(string(proto), incoming).Inc()
}

// LogSentMessage  logs the message sent to the peer.
func (b *BandwidthCollector) LogSentMessage(int64) {}

// LogRecvMessage logs the message received from the peer.
func (b *BandwidthCollector) LogRecvMessage(int64) {}

// GetBandwidthForPeer mock returns the bandwidth for a given peer.
func (b *BandwidthCollector) GetBandwidthForPeer(peer.ID) metrics.Stats {
	return metrics.Stats{}
}

// GetBandwidthForProtocol mock returns the bandwidth for a given protocol.
func (b *BandwidthCollector) GetBandwidthForProtocol(protocol.ID) metrics.Stats {
	return metrics.Stats{}
}

// GetBandwidthTotals returns mock the total bandwidth used by the node.
func (b *BandwidthCollector) GetBandwidthTotals() metrics.Stats {
	return metrics.Stats{}
}

// GetBandwidthByPeer mock returns the bandwidth for a given peer.
func (b *BandwidthCollector) GetBandwidthByPeer() map[peer.ID]metrics.Stats {
	return nil
}

// GetBandwidthByProtocol mock returns the bandwidth for a given protocol.
func (b *BandwidthCollector) GetBandwidthByProtocol() map[protocol.ID]metrics.Stats {
	return nil
}
