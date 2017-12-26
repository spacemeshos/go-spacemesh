package p2p

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
)

// This file contain swarm internal handlers
// These should only be called from the swarms event processing main loop
// or by other internal handlers but not from a random type or go routine

// Handles local request to register a remote node in the swarm
// Register adds info about this node but doesn't attempt to connect to i
func (s *swarmImpl) onRegisterNodeRequest(n node.RemoteNodeData) {

	if s.peers[n.Id()] != nil {
		s.localNode.Info("Already connected to %s", n.Id())
		return
	}

	rn, err := NewRemoteNode(n.Id(), n.Ip())
	if err != nil { // invalid id
		return
	}

	s.peers[n.Id()] = rn

	// update the routing table with the nde node info
	s.routingTable.Update(n)

}

// Handle local request to connect to a remote node
func (s *swarmImpl) onConnectionRequest(req node.RemoteNodeData) {

	var err error

	// check for existing session with that node
	peer := s.peers[req.Id()]

	if peer == nil {
		peer, err = NewRemoteNode(req.Id(), req.Ip())
		if err != nil {
			return
		}

		// store new remote node by id
		s.peers[req.Id()] = peer
	}

	conn := peer.GetActiveConnection()
	session := peer.GetAuthenticatedSession()

	if conn != nil && session != nil {
		// we have a connection with the node and an active session
		return
	}

	if conn == nil {

		// Dial the other node using the node's network config values
		conn, err = s.network.DialTCP(req.Ip(), s.localNode.Config().DialTimeout, s.localNode.Config().ConnKeepAlive)
		if err != nil {
			// todo: we need to fire an event so app-level code knows about failure to connect
			s.localNode.Error("failed to connect to remote node on advertised ip %s", req.Ip)
			return
		}

		// update the routing table
		s.routingTable.Update(req)

		id := conn.Id()

		// update state with new connection
		s.peersByConnection[id] = peer
		s.connections[id] = conn

		// update remote node connections
		peer.GetConnections()[id] = conn
	}

	// todo: we need to handle the case that there's a non-authenticated session with the remote node
	// we need to decide if to wait for it to auth, kill it, etc....
	if session == nil || !session.IsAuthenticated() {

		// start handshake protocol
		s.handshakeProtocol.CreateSession(peer)
	}
}

// callback from handshake protocol when session state changes
func (s *swarmImpl) onNewSession(data HandshakeData) {

	if data.Session().IsAuthenticated() {
		s.localNode.Info("Established new session with %s", data.Peer().TcpAddress())

		// store the session
		s.allSessions[data.Session().String()] = data.Session()

		// send all messages queued for the remote node we now have a session with
		for key, msg := range s.messagesPendingSession {
			if msg.PeerId == data.Peer().String() {
				s.localNode.Info("Sending queued message %s to remote node", hex.EncodeToString(msg.ReqId))
				delete(s.messagesPendingSession, key)
				go s.SendMessage(msg)
			}
		}
	}
}

// Local request to disconnect from a node
func (s *swarmImpl) onDisconnectionRequest(req node.RemoteNodeData) {
	// disconnect from node...
}

// Local request to send a message to a remote node
func (s *swarmImpl) onSendHandshakeMessage(r SendMessageReq) {

	// check for existing remote node and session
	remoteNode := s.peers[r.PeerId]

	if remoteNode == nil {
		// for now we assume messages are sent only to nodes we already know their ip address
		return
	}

	conn := remoteNode.GetActiveConnection()

	if conn == nil {
		s.localNode.Error("Expected to have a connection with remote node")
		return
	}

	conn.Send(r.Payload, r.ReqId)
}

// Local request to send a message to a remote node
func (s *swarmImpl) onSendMessageRequest(r SendMessageReq) {

	// check for existing remote node and session
	peer := s.peers[r.PeerId]

	if peer == nil {

		s.localNode.Info("No contact info to target peer - attempting to find it on the network...")
		callback := make(chan node.RemoteNodeData)

		// attempt to find the peer
		s.findNode(r.PeerId, callback)
		go func() {
			select {
			case n := <-callback:
				if n != nil { // we found it - now we can send the message to it
					log.Info("Peer %s found.... - sending message", r.PeerId)
					s.onSendMessageRequest(r)
				} else {
					log.Info("Peer %s not found.... - can't send message", r.PeerId)
				}
			}
		}()

		return
	}

	session := peer.GetAuthenticatedSession()
	conn := peer.GetActiveConnection()

	if session == nil || conn == nil {

		s.localNode.Warning("queuing protocol request until session is established...")

		// save the message for later sending and try to connect to the node
		s.messagesPendingSession[hex.EncodeToString(r.ReqId)] = r

		// try to connect to remote node and send the message once connected
		// todo: callback listener if connection fails (possibly after retries)
		s.onConnectionRequest(node.NewRemoteNodeData(peer.String(), peer.TcpAddress()))
		return
	}

	encPayload, err := session.Encrypt(r.Payload)
	if err != nil {
		e := errors.New(fmt.Sprintf("aborting send - failed to encrypt payload: %v", err))
		go func() {
			if r.Callback != nil {
				r.Callback <- SendError{r.ReqId, e}
			}
		}()
		return
	}

	msg := &pb.CommonMessageData{
		SessionId: session.Id(),
		Payload:   encPayload,
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		e := errors.New(fmt.Sprintf("aborting send - invalid msg format %v", err))
		go func() {
			if r.Callback != nil {
				r.Callback <- SendError{r.ReqId, e}
			}
		}()
		return
	}

	// store callback by reqId for this connection so we can call back in case of msg timout or other send failure
	if r.Callback != nil {
		callbacks := s.outgoingSendsCallbacks[conn.Id()]
		if callbacks == nil {
			s.outgoingSendsCallbacks[conn.Id()] = make(map[string]chan SendError)
		}

		s.outgoingSendsCallbacks[conn.Id()][hex.EncodeToString(r.ReqId)] = r.Callback
	}

	// finally - send it away!
	s.localNode.Info("Sending protocol message down the connection...")

	conn.Send(data, r.ReqId)
}

func (s *swarmImpl) onConnectionClosed(c net.Connection) {

	// forget about this connection
	id := c.Id()
	peer := s.peersByConnection[id]
	if peer != nil {
		peer.GetConnections()[id] = nil
	}
	delete(s.connections, id)
	delete(s.peersByConnection, id)
}

func (s *swarmImpl) onRemoteClientConnected(c net.Connection) {
	// nop - a remote client connected - this is handled w message
	s.localNode.Info("Remote client connected. %s", c.Id())
}

// Processes an incoming handshake protocol message
func (s *swarmImpl) onRemoteClientHandshakeMessage(msg net.IncomingMessage) {

	data := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Message, data)
	if err != nil {
		s.localNode.Warning("unexpected handshake message format: %v", err)
		return
	}

	connId := msg.Connection.Id()

	// check if we already know about the remote node of this connection
	sender := s.peersByConnection[connId]

	if sender == nil {

		// authenticate sender before registration
		err := authenticateSenderNode(data)
		if err != nil {
			s.localNode.Error("Dropping incoming message - failed to authenticate message sender: %v", err)
			return
		}

		nodeKey, err := crypto.NewPublicKey(data.NodePubKey)
		if err != nil {
			return
		}

		sender, err = NewRemoteNode(nodeKey.String(), data.TcpAddress)
		if err != nil {
			return
		}

		// register this remote node and the new connection

		sender.GetConnections()[connId] = msg.Connection
		s.peers[sender.String()] = sender
		s.peersByConnection[connId] = sender

	}

	// update the routing table - we just heard from this node
	s.routingTable.Update(sender.GetRemoteNodeData())

	// Let the demuxer route the message to its registered protocol handler
	s.demuxer.RouteIncomingMessage(NewIncomingMessage(sender, data.Protocol, msg.Message))
}

// Pre-process a protocol message from a remote client handling decryption and authentication
// Authenticated messages are forwarded to the demuxer for demuxing to protocol handlers
func (s *swarmImpl) onRemoteClientProtocolMessage(msg net.IncomingMessage, c *pb.CommonMessageData) {

	// Locate the session
	session := s.allSessions[hex.EncodeToString(c.SessionId)]

	if session == nil || !session.IsAuthenticated() {
		s.localNode.Warning("Dropping incoming protocol message - expected to have an authenticated session with this node")
		return
	}

	remoteNode := s.peers[session.RemoteNodeId()]
	if remoteNode == nil {
		s.localNode.Warning("Dropping incoming protocol message - expected to have data about this node for an established session")
		return
	}

	decPayload, err := session.Decrypt(c.Payload)
	if err != nil {
		s.localNode.Warning("Droping incoming protocol message - can't decrypt message payload with session key. %v", err)
		return
	}

	pm := &pb.ProtocolMessage{}
	err = proto.Unmarshal(decPayload, pm)
	if err != nil {
		s.localNode.Warning("Droping incoming protocol message - Failed to get protocol message from payload. %v", err)
		return
	}

	// authenticate message author - we already authenticated the sender via the shared session key secret
	err = pm.AuthenticateAuthor()
	if err != nil {
		s.localNode.Warning("Droping incoming protocol message - Failed to verify author. %v", err)
		return
	}

	s.localNode.Info("Message %s author authenticated ok. %s", hex.EncodeToString(pm.GetMetadata().ReqId), pm.GetMetadata().Protocol)

	// todo: check send time and reject if too much aparat from local clock

	// update the routing table - we just heard from this authenticated node
	s.routingTable.Update(remoteNode.GetRemoteNodeData())

	// todo: remove send error channels for this connection if this is a response to a locally sent request

	// route authenticated message to the reigstered protocol
	s.demuxer.RouteIncomingMessage(NewIncomingMessage(remoteNode, pm.Metadata.Protocol, decPayload))

}

// Main incoming network messages handler
// c: connection we got this message on
// msg: binary protobufs encoded data
//
// not go safe - called from event processing main loop
func (s *swarmImpl) onRemoteClientMessage(msg net.IncomingMessage) {

	c := &pb.CommonMessageData{}
	err := proto.Unmarshal(msg.Message, c)
	if err != nil {
		s.localNode.Warning("Bad request - closing connection...")
		msg.Connection.Close()
		return
	}

	// route messages based on msg payload length
	if len(c.Payload) == 0 {
		// handshake messages have no enc payload
		s.onRemoteClientHandshakeMessage(msg)

	} else {
		// protocol messages are encrypted in payload
		s.onRemoteClientProtocolMessage(msg, c)
	}
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onMessageSentEvent(evt net.MessageSentEvent) {

	// message was written to a connection without an error
	callbacks := s.outgoingSendsCallbacks[evt.Connection.Id()]
	if callbacks == nil {
		return
	}

	reqId := hex.EncodeToString(evt.Id)
	callback := callbacks[reqId]
	if callback == nil {
		return
	}

	// stop tracking this outgoing message - it was sent
	delete(callbacks, reqId)
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onConnectionError(ce net.ConnectionError) {
	s.sendMessageSendError(ce)
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onMessageSendError(mse net.MessageSendError) {
	s.sendMessageSendError(net.ConnectionError{mse.Connection, mse.Err, mse.Id})
}

// send a msg send error back to the callback registered by reqId
func (s *swarmImpl) sendMessageSendError(cr net.ConnectionError) {

	c := cr.Connection
	callbacks := s.outgoingSendsCallbacks[c.Id()]

	if callbacks == nil {
		return
	}

	reqId := hex.EncodeToString(cr.Id)
	callback := callbacks[reqId]

	if callback == nil {
		return
	}

	// there will be no more callbacks for this reqId
	delete(callbacks, reqId)

	go func() {
		// send the error to the callback channel
		callback <- SendError{cr.Id, cr.Err}
	}()
}
