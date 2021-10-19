// Package metrics defines metric reporting for the p2p component.
package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem = "p2p"

	// TypeLabel is the metrics label name for type of connection.
	TypeLabel = "type"
	// TypeLabelInbound is the label value for inbound connection.
	TypeLabelInbound = "inbound"
	// TypeLabelOutbound is the label value for outbound connection.
	TypeLabelOutbound = "outbound"

	// MessageTypeLabel is the metrics label name for type of gossip messages.
	MessageTypeLabel = "message_type"
	// MessageTypeLabelOld is the label value for gossip messages already seen before.
	MessageTypeLabelOld = "old"
	// MessageTypeLabelNew is the label value for gossip messages not seen before.
	MessageTypeLabelNew = "new"

	// ProtocolLabel holds the name we use to add a protocol label value.
	ProtocolLabel = "protocol"

	// PeerIDLabel holds the name we use to add a protocol label value.
	PeerIDLabel = "peer_id"
)

var (
	// PropagationQueueLen is the current size of the gossip queue.
	PropagationQueueLen = metrics.NewGauge(
		"propagate_queue_len",
		subsystem,
		"Number of messages in the gossip queue",
		nil,
	)
	// QueueLength is the current size of protocol queues.
	QueueLength = metrics.NewGauge(
		"protocol_queue_len",
		subsystem,
		"Length of protocol queues",
		[]string{ProtocolLabel},
	)

	// TotalPeers is the number of peers the node has connected.
	TotalPeers = metrics.NewGauge(
		"peers",
		subsystem,
		"Number of peers",
		[]string{TypeLabel},
	)

	// PeerRecv is the num of bytes received from peer.
	PeerRecv = metrics.NewCounter(
		"peer_receive_bytes_total",
		subsystem,
		"Number of bytes received from a given peer",
		[]string{PeerIDLabel},
	)

	// PeerSend is the num of bytes sent to peer.
	PeerSend = metrics.NewCounter(
		"peer_send_bytes_total",
		subsystem,
		"Number of bytes sent to a given peer",
		[]string{PeerIDLabel},
	)

	// TotalGossipMessages is the number of received gossip messages.
	TotalGossipMessages = metrics.NewCounter(
		"total_gossip_messages",
		subsystem,
		"Number of gossip messages received",
		[]string{ProtocolLabel, MessageTypeLabel},
	)
)
