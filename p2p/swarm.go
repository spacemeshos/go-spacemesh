package p2p

import (
	"github.com/UnrulyOS/go-unruly/p2p/dht/table"
	"github.com/UnrulyOS/go-unruly/p2p/node"
)

// Swarm
// A p2p network of unruly nodes

type Swarm interface {

	// find a specific node based its id
	// id - base58 encoded node id
	FindNode(id string, callback chan node.RemoteNodeData)

	// Register a node with the swarm based on its id and ip address but don't attempt to connect to it
	RegisterNode(data node.RemoteNodeData)

	// Attempt to establish a session with a remote node with a known ip address - useful for bootstrapping
	ConnectTo(req node.RemoteNodeData)

	//Connect to count random nodde
	ConnectToRandomNodes(count int, callback chan node.RemoteNodeData)

	// forcefully disconnect form a node - close any connections and sessions with it
	DisconnectFrom(req node.RemoteNodeData)

	// Send a message to a specific remote node - ideally we want to enable sending to any node
	// without knowing its ip address - in this case we will try to locate the node via dht node search
	// and send the message if we obtained node ip address and were able to connect to it
	// req.msg should be marshaled protocol message. e.g. something like pb.PingReqData
	// This is design for standard messages that require a session
	SendMessage(req SendMessageReq)

	// Send a handshake protocol message that is used to establish a session
	SendHandshakeMessage(req SendMessageReq)

	GetDemuxer() Demuxer

	GetLocalNode() LocalNode

	getHandshakeProtocol() HandshakeProtocol

	getRoutingTable() table.RoutingTable

	getFindNodeProtocol() FindNodeProtocol
}

type SendMessageReq struct {
	PeerId  string // string encoded key
	ReqId   []byte
	Payload []byte // this should be a marshaled protocol msg e.g. PingReqData
}

type NodeResp struct {
	peerId string
	err    error
}
