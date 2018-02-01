// Package p2p implement the core spacemesh p2p protocol and provide types for higher-level protcols such as handshake.
// See Ping for an example higher-level protocol
package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Swarm is p2p virtual network of spacemesh nodes as viewed by a local node
type Swarm interface {

	// Send a message to a specific remote node based just on its id without knowing its ip address
	// In this case we will try to locate the node via dht node search
	// and send the message if we obtained node ip address and were able to connect to it
	// req.msg should be marshaled protocol message. e.g. something like pb.PingReqData
	// This is designed for standard messages that require a session
	// This method is designed to be used by protocols implementations to send message to any destination
	SendMessage(req SendMessageReq)

	// TODO: support sending a message to all connected peers without any dest id provided by caller

	// Register a node with the swarm based on its id and tcp address but don't attempt to connect to it
	RegisterNode(data node.RemoteNodeData)

	// Attempt to establish a session with a remote node with a known id and tcp address
	// Used for bootstrapping known bootstrap nodes
	ConnectTo(req node.RemoteNodeData)

	// Connect to count random nodes - used for bootstrapping the swarm
	ConnectToRandomNodes(count int)

	// Forcefully disconnect form a node - close any connections and sessions with it
	DisconnectFrom(req node.RemoteNodeData)

	// Send a handshake protocol message that is used to establish a session
	sendHandshakeMessage(req SendMessageReq)

	// Register a callback channel for state changes related to remote nodes
	// Currently used for testing network bootstrapping
	RegisterNodeEventsCallback(callback NodeEventCallback)

	// todo: allow client to register callback to get notified when swarm has shut down
	Shutdown()

	// services getters
	GetDemuxer() Demuxer
	GetLocalNode() LocalNode

	// used by protocols and for testing
	getHandshakeProtocol() HandshakeProtocol
	getRoutingTable() table.RoutingTable
	getFindNodeProtocol() FindNodeProtocol
}

// SendMessageReq specifies data required for sending a p2p message to a remote peer.
type SendMessageReq struct {
	PeerID   string         // base58 message destination peer id
	ReqID    []byte         // unique request id
	Payload  []byte         // this should be a marshaled protocol msg e.g. PingReqData
	Callback chan SendError // optional callback to receive send errors or timeout
}

// SendError specifies message sending error data.
type SendError struct {
	ReqID []byte // unique request id
	err   error  // error - nil if message was sent
}

// NodeResp defines node response data
type NodeResp struct {
	peerID string
	err    error
}

// NodeEvent specifies node state data
type NodeEvent struct {
	PeerID string
	State  NodeState
}

// NodeEventCallback is a channel of NodeEvents
type NodeEventCallback chan NodeEvent

// NodeState specifies the node's connectivity state.
type NodeState int32

// NodeState
const (
	Unknown NodeState = iota
	Registered
	Connecting
	Connected
	HandshakeStarted
	SessionEstablished
	Disconnected
)
