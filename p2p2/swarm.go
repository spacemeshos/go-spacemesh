package p2p2

// Swarm
// Hold ref to peers and manage connections to peers

// unruly p2p2 Stack:
// -----------
// Local Node
// -- Implements p2p protocols: impls with req/resp support (match id resp w request id)
//	 -- ping protocol
//   -- echo protocol
//   -- all other protocols...
// DeMuxer (node aspect) - routes remote requests (and responses) back to protocols. Protocols register on muxer
// Swarm forward incoming messages to muxer. Let protocol send message to a remote node. Connects to random nodes.
// Swarm - managed all remote nodes, sessions and connections
// Remote Node - maintains sessions - should be internal to swarm only. Remote node id str only above this
// -- NetworkSession (optional)
// Connection
// -- msgio (prefix length encoding). shared buffers.
// Network
//	- tcp for now
//  - utp / upd soon

type Swarm interface {

	// register a node with the swarm based on id and ip address
	RegisterNode(data RemoteNodeData)

	// Attempt to establish a session with a remote node with a known ip address - useful for bootstrapping
	ConnectTo(req RemoteNodeData)

	// ConnectToRandomNodes(maxNodes int) Get random nodes (max int) get up to max random nodes from the swarm

	// todo: add find node data using dht to obtain the ip address of a remote node with only known id
	// LocateRemoteNode(nodeId string)

	// forcefully disconnect form a node - close any connections and sessions with it
	DisconnectFrom(req RemoteNodeData)

	// Send a message to a remote node - ideally we want to enable sending to any node
	// without knowing his ip address - in this case we will try to locate the node via dht node search
	// and send the message if we obtained node ip address and were able to connect to it
	// req.msg should be marshaled protocol message. e.g. something like pb.PingReqData
	SendMessage(req SendMessageReq)

	GetDemuxer() Demuxer

	LocalNode() LocalNode
}

// outside of swarm - types only know about this and not about RemoteNode
type RemoteNodeData struct {
	id string
	ip string
}

type SendMessageReq struct {
	remoteNodeId string // string encoded key
	reqId        string
	payload      []byte // this should be marshaled protocol msg e.g. PingReqData
}

type NodeResp struct {
	remoteNodeId string
	err          error
}
