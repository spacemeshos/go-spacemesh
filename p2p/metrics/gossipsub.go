package metrics

import (
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

var (
	totalPeers       = metrics.NewGauge("total_peers", subsystem, "Total number of peers", nil)
	peersPerProtocol = metrics.NewGauge("peers_per_protocol", subsystem, "Number of peers per protocol", []string{"protocol"})
)

var (
	// ProcessedMessagesDuration in nanoseconds to process a message. Labeled by protocol and result.
	ProcessedMessagesDuration = metrics.NewHistogramWithBuckets(
		"processed_messages_duration",
		subsystem,
		"Duration in nanoseconds to process a message",
		[]string{"protocol", "result"},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	)
	deliveredMessagesBytes = metrics.NewCounter(
		"delivered_messages_bytes",
		subsystem,
		"Total amount of delivered payloads (doesn't count gossipsub metadata)",
		[]string{"protocol"},
	)
	receivedMessagesBytes = metrics.NewCounter(
		"received_messages_bytes",
		subsystem,
		"Total amount of received payloads (doesn't count gossipsub metadata)",
		[]string{"protocol"},
	)
	deliveredMessagesCount = metrics.NewCounter(
		"delivered_messages_count",
		subsystem,
		"Total number of delivered messages",
		[]string{"protocol"},
	)
	receivedMessagesCount = metrics.NewCounter(
		"received_messages_count",
		subsystem,
		"Total amount of received messages",
		[]string{"protocol"},
	)
)

// GossipCollector pubsub.RawTracer implementation
// total number of peers
// number of peers per each gossip protocol.
type GossipCollector struct {
	peers struct {
		sync.Mutex
		m map[peer.ID]protocol.ID
	}
}

// NewGoSIPCollector creates a new GossipCollector.
func NewGoSIPCollector() *GossipCollector {
	return &GossipCollector{
		peers: struct {
			sync.Mutex
			m map[peer.ID]protocol.ID
		}{
			m: make(map[peer.ID]protocol.ID),
		},
	}
}

// AddPeer is invoked when a new peer is added.
func (g *GossipCollector) AddPeer(id peer.ID, proto protocol.ID) {
	g.peers.Lock()
	g.peers.m[id] = proto
	g.peers.Unlock()

	peersPerProtocol.WithLabelValues(string(proto)).Inc()
	totalPeers.WithLabelValues().Inc()
}

// RemovePeer is invoked when a peer is removed.
func (g *GossipCollector) RemovePeer(id peer.ID) {
	g.peers.Lock()
	proto := g.peers.m[id]
	delete(g.peers.m, id)
	g.peers.Unlock()

	peersPerProtocol.WithLabelValues(string(proto)).Dec()
	totalPeers.WithLabelValues().Dec()
}

// Join is invoked when a new topic is joined.
func (g *GossipCollector) Join(string) {}

// Leave is invoked when a topic is abandoned.
func (g *GossipCollector) Leave(string) {}

// Graft is invoked when a new peer is grafted on the mesh (gossipsub).
func (g *GossipCollector) Graft(peer.ID, string) {}

// Prune is invoked when a peer is pruned from the message (gossipsub).
func (g *GossipCollector) Prune(peer.ID, string) {}

// ValidateMessage is invoked when a message first enters the validation pipeline.
func (g *GossipCollector) ValidateMessage(msg *pubsub.Message) {
	if msg.Topic == nil {
		return
	}
	receivedMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	receivedMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

// DeliverMessage is invoked when a message is delivered.
func (g *GossipCollector) DeliverMessage(msg *pubsub.Message) {
	if msg.Topic == nil {
		return
	}
	deliveredMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	deliveredMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

// RejectMessage is invoked when a message is Rejected or Ignored.
// The reason argument can be one of the named strings Reject*.
func (g *GossipCollector) RejectMessage(*pubsub.Message, string) {}

// DuplicateMessage is invoked when a duplicate message is dropped.
func (g *GossipCollector) DuplicateMessage(msg *pubsub.Message) {
	if msg.Topic == nil {
		return
	}
	receivedMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	receivedMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

// ThrottlePeer is invoked when a peer is throttled by the peer gater.
func (g *GossipCollector) ThrottlePeer(peer.ID) {}

// RecvRPC is invoked when an incoming RPC is received.
func (g *GossipCollector) RecvRPC(*pubsub.RPC) {}

// SendRPC is invoked when a RPC is sent.
func (g *GossipCollector) SendRPC(*pubsub.RPC, peer.ID) {}

// DropRPC is invoked when an outbound RPC is dropped, typically because of a queue full.
func (g *GossipCollector) DropRPC(*pubsub.RPC, peer.ID) {}

// UndeliverableMessage is invoked when the consumer of Subscribe is not reading messages fast enough and
// the pressure release mechanism trigger, dropping messages.
func (g *GossipCollector) UndeliverableMessage(*pubsub.Message) {}
