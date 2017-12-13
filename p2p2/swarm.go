package p2p2

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/pb"
	"github.com/gogo/protobuf/proto"
	"time"
)

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

// handle local request to register a remote node
func (s *swarmImpl) onRegisterNodeRequest(req RemoteNodeData) {

	if s.peers[req.id] == nil {
		node, err := NewRemoteNode(req.id, req.ip)
		if err != nil {
			// invalid id
			return
		}

		s.peers[req.id] = node
	}
}

// handle local request to connect to a node
func (s *swarmImpl) onConnectionRequest(req RemoteNodeData) {

	// check for existing session
	remoteNode := s.peers[req.id]

	if remoteNode == nil {

		remoteNode, err := NewRemoteNode(req.id, req.ip)
		if err != nil {
			return
		}

		// store new remote node by id
		s.peers[req.id] = remoteNode
	}

	conn := remoteNode.GetActiveConnection()
	session := remoteNode.GetAuthenticatedSession()

	if conn != nil && session != nil {
		// we have a connection with the node and an active session
		return
	}

	if conn == nil {
		conn, err := s.network.DialTCP(req.ip, time.Duration(10*time.Second))
		if err != nil {
			// log it here
			log.Error("failed to connect to remote node on advertised ip %s", req.ip)
			return
		}

		id := conn.Id()

		// update state with new connection
		s.peersByConnection[id] = remoteNode
		s.connections[id] = conn

		// update remote node connections
		remoteNode.GetConnections()[id] = conn
	}

	// todo: we need to handle the case that there's a non-authenticated session with the remote node
	// we need to decide if to wait for it to auth, kill it, etc....
	if session == nil {

		// start handshake
		s.handshakeProtocol.CreateSession(remoteNode)
	}
}

// callback from handshake protocol when session state changes
func (s *swarmImpl) onNewSession(session *NewSessoinData) {

	if session.session.IsAuthenticated() {
		s.allSessions[session.session.String()] = session.session
		// todo: if a session authenticated send out all pending messages to this node from its queue

	}
}

// local request to disconnect from a node
func (s *swarmImpl) onDisconnectionRequest(req RemoteNodeData) {
	// disconnect from node...
}

// a local request to send a message to a node
func (s *swarmImpl) onSendMessageRequest(r SendMessageReq) {

	// check for existing remote node and session
	remoteNode := s.peers[r.remoteNodeId]

	if remoteNode == nil {
		// for now we assume messages are sent only to nodes we already know their ip address
		return
	}

	session := remoteNode.GetAuthenticatedSession()
	conn := remoteNode.GetActiveConnection()

	if session == nil || conn == nil {
		// save the message for later sending and try to connect to the node
		s.messagesPendingSession[r.reqId] = r

		// try to connect to remote node and send the message once connceted
		s.onConnectionRequest(RemoteNodeData{remoteNode.String(), remoteNode.TcpAddress()})
		return
	}

	// todo: generate payload - encrypt r.msg with session aes enc key using session + hmac
	// we need to keep an AES dec/enc with the session and use it
	var encPayload = r.payload

	msg := &pb.CommonMessageData{
		SessionId: session.Id(),
		Payload:   encPayload,
	}

	data, err := proto.Marshal(msg)

	if err != nil {
		log.Error("aborting send - invalid msg format %v", err)
		return
	}

	// finally - send it away!
	conn.Send(data)
}

func (s *swarmImpl) onConnectionClosed(c Connection) {
	delete(s.connections, c.Id())
	delete(s.peersByConnection, c.Id())
}

func (s *swarmImpl) onRemoteClientConnected(c Connection) {
	// nop - a remote client connected - this is handled w message
}

// Handles an incoming handshake protocol message (pre-processing)
func (s *swarmImpl) onRemoteClientHandshakeMessage(msg ConnectionMessage) {

	data := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Message, data)
	if err != nil {
		log.Warning("unexpected handshake message format: %v", err)
		return
	}

	// check if we already know about the remote node of this connection
	sender := s.peersByConnection[msg.Connection.Id()]

	if sender == nil {
		// authenticate sender before registration
		err := AuthenticateSenderNode(data)
		if err != nil {
			log.Error("failed to authenticate message sender %v", err)
			return
		}

		nodeKey, err := NewPublicKey(data.NodePubKey)
		if err != nil {
			return
		}

		sender, err = NewRemoteNode(nodeKey.String(), data.TcpAddress)
		if err != nil {
			return
		}

		// register this remote node and its connection
		s.peers[sender.String()] = sender
		s.peersByConnection[msg.Connection.Id()] = sender

	}

	// node that we always have a proper remote node sender data, let the demuxer route to the handshake protocol handler
	s.demuxer.RouteIncomingMessage(IncomingMessage{sender: sender, msg: msg.Message, protocol: data.Protocol})
}

func (s *swarmImpl) onRemoteClientProtocolMessage(msg ConnectionMessage, c *pb.CommonMessageData) {
	// just find the session here
	session := s.allSessions[hex.EncodeToString(c.SessionId)]

	if session == nil {
		log.Warning("expected to have an authenticated session with this node")
		return
	}

	remoteNode := s.peers[session.RemoteNodeId()]
	if remoteNode == nil {
		log.Warning("expected to have data about this node")
		return
	}

	// todo: attempt to decrypt message (c.payload) with active session key

	// todo: create pb.ProtocolMessage from the decrypted message

	// todo: end to muxer pb.protocolMessage to the muxer for handling - it has protocol, reqId, sessionId, etc....

	//data := proto.Unmarshal(msg.Message, proto.Message)

	// 1. decyrpt protobuf to a generic protobuf obj - all messages are protobufs but we don't know struct type just yet

	// there are 2 types of high-level payloads: session establishment handshake (req or resp)
	// and messages encrypted using session id

	// 2. if session id is included in message and connection has this key then use it to decrypt payload - other reject and close conn

	// 3. if req is a handshake request or response then handle it to establish a session - use code here or use handshake protocol handler

	// 4. if req is from a remote node we don't know about so create it

	// 5. save connection for this remote node - one connection per node for now

	// 6. auth message sent by remote node pub key close connection otherwise

	// use Demux.RouteIncomingMessage ()

}

// Main network messages handler
// c: connection we got this message on
// msg: binary protobufs encoded data
// not go safe - called from event processing main loop
func (s *swarmImpl) onRemoteClientMessage(msg ConnectionMessage) {

	c := &pb.CommonMessageData{}
	err := proto.Unmarshal(msg.Message, c)
	if err != nil {
		log.Warning("Bad request - closing connection...")
		msg.Connection.Close()
		return
	}

	if len(c.Payload) == 0 {
		s.onRemoteClientHandshakeMessage(msg)

	} else {
		s.onRemoteClientProtocolMessage(msg, c)
	}
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onConnectionError(err ConnectionError) {
	// close the connection?
	// who to notify?
	// update remote node?
	// retry to connect to node?
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onMessageSendError(err MessageSendError) {
	// what to do here - retry ?
}
