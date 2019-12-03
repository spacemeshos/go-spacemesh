package metrics

import "github.com/spacemeshos/go-spacemesh/metrics"

const (
	// Namespace is the metrics namespace //todo: figure out if this can be used better.
	Namespace = "spacemesh"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "p2p"

	typeLabel        = "type"
	messageTypeLabel = "message_type"

	// ProtocolLabel holds the name we use to add a protocol label value
	ProtocolLabel = "protocol"

	// PeerIdLabel holds the name we use to add a protocol label value
	PeerIdLabel = "peer_id"
)

var (
	// totalPeers is the total number of peers we have connected
	totalPeers = metrics.NewGauge("peers", MetricsSubsystem, "Number of connected peers", []string{typeLabel})
	// OutboundPeers is the number of outbound peers we have connected
	OutboundPeers = totalPeers.With(typeLabel, "outbound")
	// InboundPeers is the number of inbound peers we have connected
	InboundPeers = totalPeers.With(typeLabel, "inbound")
	// PeerRecv is the num of bytes received from peer
	PeerRecv = metrics.NewCounter("peer_receive_bytes_total", MetricsSubsystem, "Number of bytes received from a given peer.", []string{PeerIdLabel})
	// PeerSend is the num of bytes sent to peer
	PeerSend = metrics.NewCounter("peer_send_bytes_total", MetricsSubsystem, "Number of bytes sent to a given peer.", []string{PeerIdLabel})

	totalGossipMessages = metrics.NewCounter("total_gossip_messages", MetricsSubsystem, "number of gossip messages", []string{ProtocolLabel, messageTypeLabel})
	// NewGossipMessages is a metric for newly received gossip messages
	NewGossipMessages = totalGossipMessages.With(messageTypeLabel, "new")
	// OldGossipMessages is a metric for old messages received (duplicates)
	OldGossipMessages = totalGossipMessages.With(messageTypeLabel, "old")

	// AddrbookSize is the current size of the discovery
	AddrbookSize = metrics.NewGauge("addrbook_size", MetricsSubsystem, "Number of peers in the discovery", nil)

	// PropagationQueueLen is the current size of the gossip queue
	PropagationQueueLen = metrics.NewGauge("propagate_queue_len", MetricsSubsystem, "Number of messages in the gossip queue", nil)
	// QueueLength is the current size of protocol queues
	QueueLength = metrics.NewGauge("protocol_queue_len", MetricsSubsystem, "len of protocol queues", []string{ProtocolLabel})
)

// todo: maybe add functions that attach peer_id and protocol. (or other labels) without writing label names.
