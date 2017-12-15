package swarm

// Swarm
// A p2p network of unruly nodes

type Swarm interface {

	// Register a node with the swarm based on id and ip address - bootstrap nodes should be registered using
	// this method
	RegisterNode(data RemoteNodeData)

	// Attempt to establish a session with a remote node with a known ip address - useful for bootstrapping
	ConnectTo(req RemoteNodeData)

	// ConnectToRandomNodes(maxNodes int) Get random nodes (max int) get up to max random nodes from the swarm

	// todo: add find node data using dht to obtain the ip address of a remote node with only known id
	// LocateRemoteNode(nodeId string)

	// forcefully disconnect form a node - close any connections and sessions with it
	DisconnectFrom(req RemoteNodeData)

	// Send a message to a remote node - ideally we want to enable sending to any node
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
}

// Outside of swarm - types only know about this and not about RemoteNode
type RemoteNodeData struct {
	Id string
	Ip string
}

type SendMessageReq struct {
	RemoteNodeId string // string encoded key
	ReqId        []byte
	Payload      []byte // this should be a marshaled protocol msg e.g. PingReqData
}

type NodeResp struct {
	remoteNodeId string
	err          error
}
