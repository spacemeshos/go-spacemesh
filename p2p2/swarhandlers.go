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
