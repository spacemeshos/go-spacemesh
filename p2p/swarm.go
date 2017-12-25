package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Swarm
// p2p virtual network of unruly nodes
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
	ConnectToRandomNodes(count int, callback chan node.RemoteNodeData)

	// Forcefully disconnect form a node - close any connections and sessions with it
	DisconnectFrom(req node.RemoteNodeData)

	// Send a handshake protocol message that is used to establish a session
	sendHandshakeMessage(req SendMessageReq)

	// services getters

	GetDemuxer() Demuxer
	GetLocalNode() LocalNode

	// used by protocols and for testing
	getHandshakeProtocol() HandshakeProtocol
	getRoutingTable() table.RoutingTable
	getFindNodeProtocol() FindNodeProtocol
}

type SendMessageReq struct {
	PeerId   string         // base58 message destination peer id
	ReqId    []byte         // unique request id
	Payload  []byte         // this should be a marshaled protocol msg e.g. PingReqData
	Callback chan SendError // optional callback to receive send errors or timeout
}

type SendError struct {
	ReqId []byte // unique request id
	err   error  // error - nil if message was sent
}

type NodeResp struct {
	peerId string
	err    error
}
