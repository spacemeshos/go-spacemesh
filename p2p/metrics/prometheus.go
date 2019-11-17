package metrics

import (
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

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

// todo: maybe add functions that attach peer_id and protocol. (or other labels) without writing label names.

// TotalPeers is the total number of peers we have connected
var (
	totalPeers = prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "peers",
		Help:      "Number of peers.",
	}, []string{typeLabel})

	// OutboundPeers is the number of outbound peers we have connected
	OutboundPeers = totalPeers.With(typeLabel, "outbound")

	// InboundPeers is the number of inbound peers we have connected
	InboundPeers = totalPeers.With(typeLabel, "inbound")

	// PeerRecv is the num of bytes received from peer
	PeerRecv = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "peer_receive_bytes_total",
		Help:      "Number of bytes received from a given peer.",
	}, []string{PeerIdLabel})

	// PeerSend is the num of bytes sent to peer
	PeerSend metrics.Counter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "peer_send_bytes_total",
		Help:      "Number of bytes sent to a given peer.",
	}, []string{PeerIdLabel})

	totalGossipMessages = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "total_gossip_messages",
		Help:      "Number of gossip messages recieved",
	}, []string{ProtocolLabel, messageTypeLabel})

	// NewGossipMessages is a metric for newly received gossip messages
	NewGossipMessages = totalGossipMessages.With(messageTypeLabel, "new")
	// OldGossipMessages is a metric for old messages received (duplicates)
	OldGossipMessages = totalGossipMessages.With(messageTypeLabel, "old")

	// AddrbookSize is the current size of the discovery
	AddrbookSize = prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "addrbook_size",
		Help:      "Number of peers in the discovery",
	}, []string{})

	// GossipQueue is the current size of the gossip queue
	PropagationQueueLen = prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "propagate_queue_len",
		Help:      "Number of messages in the gossip queue",
	}, []string{})

	// Queue is the current size queues
	QueueLength = prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: MetricsSubsystem,
		Name:      "protocol_queue_len",
		Help:      "Number of messages in the p2p queue",
	}, []string{ProtocolLabel})
)
