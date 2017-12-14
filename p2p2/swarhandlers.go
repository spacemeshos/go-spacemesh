package p2p2

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/pb"
	"github.com/golang/protobuf/proto"
	"time"
)

// This file contain swarm internal handlers
// These should only be called from the swarms eventprocessing main loop
// or by ohter internal handlers but not from a random type or go routine

// Handle local request to register a remote node in swarm
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

// Handle local request to connect to a remote node
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
	if session == nil || !session.IsAuthenticated() {

		// start handshake protocol
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

// Local request to disconnect from a node
func (s *swarmImpl) onDisconnectionRequest(req RemoteNodeData) {
	// disconnect from node...
}

// Local request to send a message to a remote node
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
	encPayload, err := session.Encrypt(r.payload)
	if err != nil {
		log.Error("aborting send - failed to encrypt payload: %v", err)
		return
	}

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

	// forget about this connection
	id := c.Id()
	peer := s.peersByConnection[id]
	if peer != nil {
		peer.GetConnections()[id] = nil
	}
	delete(s.connections, id)
	delete(s.peersByConnection, id)
}

func (s *swarmImpl) onRemoteClientConnected(c Connection) {
	// nop - a remote client connected - this is handled w message
	log.Info("Remote client connected. %s", c.Id())
}

// Preprocess an incoming handshake protocol message
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

	// demux the message to the handshake protocol handler
	s.demuxer.RouteIncomingMessage(NewIncomingMessage(sender, data.Protocol, msg.Message))
}

func (s *swarmImpl) onRemoteClientProtocolMessage(msg ConnectionMessage, c *pb.CommonMessageData) {
	// just find the session here
	session := s.allSessions[hex.EncodeToString(c.SessionId)]

	if session == nil || !session.IsAuthenticated() {
		log.Warning("expected to have an authenticated session with this node")
		return
	}

	remoteNode := s.peers[session.RemoteNodeId()]
	if remoteNode == nil {
		log.Warning("expected to have data about this node for an established session")
		return
	}

	decPayload, err := session.Decrypt(c.Payload)
	if err != nil {
		log.Warning("Invalid message payload. %v", err)
		return
	}

	pm := &pb.ProtocolMessage{}
	err = proto.Unmarshal(decPayload, pm)
	if err != nil {
		log.Warning("Failed to get protocol message from payload. %v", err)
		return
	}

	// todo: validate protocol message before demuxing to higher level handler
	// 1. authenticate author (all payload data is signed by him)
	// 2. Reject if auth timestamp is too much aparat from current local time

	s.demuxer.RouteIncomingMessage(NewIncomingMessage(remoteNode, pm.Metadata.Protocol, decPayload))

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
