package p2p2

import "github.com/UnrulyOS/go-unruly/log"

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

type swarmImpl struct {

	// set in construction and immutable state
	network   Network
	localNode LocalNode
	demuxer   Demuxer

	// all data should only be accessed from methods executed by the main swarm event loop

	// Internal state not thread safe state - must be access only from methods dispatched from the internal event handler
	peers             map[string]RemoteNode // NodeId -> RemoteNode. known remote nodes. Swarm is a peer store.
	connections       map[string]Connection // ConnId -> Connection - all open connections for tracking and mngmnt
	peersByConnection map[string]RemoteNode // ConnId -> RemoteNode remote nodes indexed by their connections.

	// todo: remove unused sessions every 24 hours
	allSessions map[string]NetworkSession // SessionId -> Session data. all authenticated session

	// remote nodes maintain their connections and sessions

	messagesPendingSession map[string]SendMessageReq // k - unique req id. outgoing messages which pend an auth session with remote node to be sent out

	// comm channels
	connectionRequests chan RemoteNodeData // local requests to establish a session w a remote node
	disconnectRequests chan RemoteNodeData // local requests to kill session and disconnect from node
	sendMsgRequests    chan SendMessageReq // local request to send a message to a node and callback on error or data
	kill               chan bool           // local request to kill the swamp from outside. e.g when local node is shutting down

	registerNodeReq chan RemoteNodeData // local request to register a node based on minimal data

	// handshake protocol implementation
	handshakeProtocol HandshakeProtocol

	// handshake protocol callback - sessions updates are pushd here
	newSessions chan *NewSessoinData // gets callback from handshake protocol when new session are created and/or auth

}

func NewSwarm(tcpAddress string, l LocalNode) (Swarm, error) {

	n, err := NewNetwork(tcpAddress)
	if err != nil {
		log.Error("can't create swarm without a network: %v", err)
		return nil, err
	}

	s := &swarmImpl{
		localNode: l,
		network:   n,
		kill:      make(chan bool),

		peersByConnection:      make(map[string]RemoteNode),
		peers:                  make(map[string]RemoteNode),
		connections:            make(map[string]Connection),
		messagesPendingSession: make(map[string]SendMessageReq),
		allSessions:            make(map[string]NetworkSession),

		connectionRequests: make(chan RemoteNodeData, 10),
		disconnectRequests: make(chan RemoteNodeData, 10),
		sendMsgRequests:    make(chan SendMessageReq, 20),
		demuxer:            NewDemuxer(),
		newSessions:        make(chan *NewSessoinData, 10),
		registerNodeReq:    make(chan RemoteNodeData, 10),
	}

	log.Info("Created swarm %s for local node %s", tcpAddress, l.String())

	s.handshakeProtocol = NewHandshakeProtocol(s)
	s.handshakeProtocol.RegisterNewSessionCallback(s.newSessions)

	go s.beginProcessingEvents()

	return s, err
}

// register a node with the swarm
func (s *swarmImpl) RegisterNode(data RemoteNodeData) {
	s.registerNodeReq <- data
}

func (s *swarmImpl) ConnectTo(req RemoteNodeData) {
	s.connectionRequests <- req
}

func (s *swarmImpl) DisconnectFrom(req RemoteNodeData) {
	s.disconnectRequests <- req
}

func (s *swarmImpl) GetDemuxer() Demuxer {
	return s.demuxer
}

func (s *swarmImpl) LocalNode() LocalNode {
	return s.localNode
}

// Send a message to a remote node
// Swarm will establish session if needed or use an existing session and open connection
// Designed to be used by any high level protocol
// req.reqId: globally unique id string - used for tracking messages we didn't get a response for yet
// req.msg: marshaled message data
// req.destId: receiver remote node public key/id

func (s *swarmImpl) SendMessage(req SendMessageReq) {
	s.sendMsgRequests <- req
}

// Swarm serial event processing
// provides concurrency safety as only one callback is executed at a time
// so there's no need for sync internal data structures
func (s *swarmImpl) beginProcessingEvents() {

Loop:
	for {
		select {
		case <-s.kill:
			// todo: gracefully stop the swarm - close all connections to remote nodes
			break Loop

		case c := <-s.network.GetNewConnections():
			s.onRemoteClientConnected(c)

		case m := <-s.network.GetIncomingMessage():
			s.onRemoteClientMessage(m)

		case err := <-s.network.GetConnectionErrors():
			s.onConnectionError(err)

		case err := <-s.network.GetMessageSendErrors():
			s.onMessageSendError(err)

		case c := <-s.network.GetClosingConnections():
			s.onConnectionClosed(c)

		case r := <-s.sendMsgRequests:
			s.onSendMessageRequest(r)

		case n := <-s.connectionRequests:
			s.onConnectionRequest(n)

		case n := <-s.disconnectRequests:
			s.onDisconnectionRequest(n)

		case session := <-s.newSessions:
			s.onNewSession(session)

		case d := <-s.registerNodeReq:
			s.onRegisterNodeRequest(d)
		}
	}
}
