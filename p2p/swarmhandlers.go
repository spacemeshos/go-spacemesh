package p2p

import (
	"encoding/hex"
	"fmt"
	"time"

	"errors"

	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/timesync"
)

// This source file contain swarm internal handlers
// These should only be called from the swarms event processing main loop
// or by other internal handlers but not from a random type or go routine

// Handles a local request to register a remote node in the swarm
// Register adds info about this node but doesn't attempt to connect to it
func (s *swarmImpl) onRegisterNodeRequest(n node.Node) {
	s.peerMapMutex.Lock()
	if _, ok := s.peers[n.String()]; ok {
		s.peerMapMutex.Unlock()
		s.localNode.Debug("Already connected to %s", n.Pretty())
		return
	}

	rn, err := NewRemoteNode(n.String(), n.Address())
	if err != nil { // invalid id
		s.peerMapMutex.Unlock()
		s.localNode.Error("invalid remote node id: %s", n.String())
		return
	}

	s.peers[n.String()] = rn
	s.peerMapMutex.Unlock()
	// update the routing table with the nde node info
	//todo : check if nessecary s.routingTable.Update(n)

	s.sendNodeEvent(n.String(), Registered)

	// TODO : Take out to some routine that waits for the registered event and sends the messages
	for reqid, msg := range s.messagesPendingRegistration {
		if msg.PeerID == n.String() {
			go s.SendMessage(msg)
			delete(s.messagesPendingRegistration, reqid)
		}
	}
}

func (s *swarmImpl) addMessagePendingRegistration(req SendMessageReq) {
	s.messagesPendingRegistration[hex.EncodeToString(req.ReqID)] = req
}

func (s *swarmImpl) addIncomingPendingMessage(ipm incomingPendingMessage) {
	s.incomingPendingMutex.Lock()
	if _, ok := s.incomingPendingSession[ipm.SessionID]; !ok {
		s.incomingPendingSession[ipm.SessionID] = []net.IncomingMessage{}
	}
	s.incomingPendingSession[ipm.SessionID] = append(s.incomingPendingSession[ipm.SessionID], ipm.Message)
	s.incomingPendingMutex.Unlock()
}

type nodeAction struct {
	req  node.Node
	done chan error
}

// Handles a local request to connect to a remote node
func (s *swarmImpl) onConnectionRequest(req node.Node, done chan error) {

	s.localNode.Debug("Local request to connect to node %s", req.Pretty())

	var err error

	s.peerMapMutex.RLock()

	// check for existing session with that node
	peer := s.peers[req.String()]

	if peer == nil {
		s.peerMapMutex.RUnlock()
		s.onRegisterNodeRequest(req)
		s.peerMapMutex.RLock()
	}

	peer = s.peers[req.String()]
	s.peerMapMutex.RUnlock()
	if peer == nil {
		nullErr := errors.New("peer not registered")
		s.localNode.Error("Err: ", nullErr)
		done <- nullErr
		return
	}

	conn := peer.GetActiveConnection()
	session := peer.GetAuthenticatedSession()

	if conn != nil && session != nil {
		s.localNode.Debug("Already got an active session and connection with: %s", req.Pretty())
		done <- nil
		return
	}

	if conn == nil {

		s.sendNodeEvent(req.String(), Connecting)

		// Dial the other node using the node's network config values
		conn, err = s.network.DialTCP(req.Address(), s.config.DialTimeout, s.config.ConnKeepAlive)
		if err != nil {
			s.sendNodeEvent(req.String(), Disconnected)
			conErr := fmt.Errorf("failed to connect to host %v , err: %v", req.Pretty(), err)
			s.localNode.Error("%v", conErr)
			done <- conErr
			return
		}

		s.sendNodeEvent(req.String(), Connected)

		// update the routing table
		s.routingTable.Update(req)

		id := conn.ID()

		// update state with new connection
		s.peerByConMapMutex.Lock()
		s.peersByConnection[id] = peer
		s.connections[id] = conn
		s.peerByConMapMutex.Unlock()

		// update remote node connections
		peer.UpdateConnection(id, conn)
	}

	// todo: we need to handle the case that there's a non-authenticated session with the remote node
	// does this should really be here ? initiating a session isnt actually initiating a connection
	// we need to decide if to wait for it to auth, kill it, etc....
	if session == nil {
		nec := make(NodeEventCallback)
		s.RegisterNodeEventsCallback(nec)
		// listen to session initiation and report while releasing context
		go func(nec NodeEventCallback) {
			sessionTimeout := time.NewTimer(30 * time.Second)
		PEERLOOP:
			for {
				select {
				case ev := <-nec:
					if ev.PeerID == req.String() {
						if ev.State == SessionEstablished {
							done <- nil
							break PEERLOOP
						} else if ev.State == Disconnected {
							done <- errors.New("session disconnected")
							break PEERLOOP
						}
					}
				case <-sessionTimeout.C:
					done <- errors.New("failed to established within time limit")
					break PEERLOOP
				}
			}
			go s.RemoveNodeEventsCallback(nec)
			s.localNode.Debug("Connection Request Finished")
		}(nec)
		// start handshake protocol
		// TODO : We don't really need HandshakeStarted
		//s.sendNodeEvent(req.DhtID(), HandshakeStarted)
		s.handshakeProtocol.CreateSession(peer)
		return
	}

	if !session.IsAuthenticated() {
		// what can we do ?
		s.localNode.Debug("Session is pending")
	}
}

// callback from handshake protocol when session state changes
func (s *swarmImpl) onNewSession(data HandshakeData) {

	if err := data.GetError(); err != nil {
		s.localNode.Error("Session creation error %s", err)
		return
	}

	// store the session
	if data.Session().IsAuthenticated() {
		s.sessionsMutex.Lock()
		s.allSessions[data.Session().String()] = data.Session()
		s.sessionsMutex.Unlock()
		s.sendNodeEvent(data.Peer().String(), SessionEstablished)
		// send all messages queued for the remote node we now have a session with
		for key, msg := range s.messagesPendingSession {
			if msg.PeerID == data.Peer().String() {
				s.localNode.Debug("Sending queued message %s to remote node", hex.EncodeToString(msg.ReqID))
				go s.SendMessage(msg)
				delete(s.messagesPendingSession, key)
			}
		}
		s.incomingPendingMutex.Lock()
		if msgs, ok := s.incomingPendingSession[data.Session().String()]; ok {
			for msg := range msgs {
				go func(im net.IncomingMessage) { s.network.GetIncomingMessage() <- im }(msgs[msg])
			}
			delete(s.incomingPendingSession, data.Session().String())
		}
		s.incomingPendingMutex.Unlock()
	}
}

// Local request to disconnect from a node
func (s *swarmImpl) onDisconnectionRequest(req node.Node) {

	// todo: disconnect all connections with node

	s.sendNodeEvent(req.String(), Disconnected)
}

// Local request to send a message to a remote node
func (s *swarmImpl) onSendHandshakeMessage(r SendMessageReq) {

	// check for existing remote node and session
	s.peerMapMutex.RLock()
	remoteNode := s.peers[r.PeerID]
	s.peerMapMutex.RUnlock()

	if remoteNode == nil {
		// for now we assume messages are sent only to nodes we already know their ip address
		s.localNode.Error("Could'nt find %v, aborting handshake", log.PrettyID(r.PeerID))
		return
	}

	conn := remoteNode.GetActiveConnection()

	if conn == nil {
		s.localNode.Error("Expected to have a connection with remote node")
		return
	}

	s.localNode.Debug("Sending Handshake to %v", remoteNode.Pretty())

	conn.Send(r.Payload, r.ReqID)
}

// Local request to send a message to a remote node
func (s *swarmImpl) onSendMessageRequest(r SendMessageReq) {

	s.peerMapMutex.RLock()

	// check for existing remote node and session
	peer := s.peers[r.PeerID]
	s.peerMapMutex.RUnlock()

	if peer == nil {
		s.localNode.Debug("No contact info to target peer - attempting to find it on the network...")
		callback := make(chan node.Node)

		// attempt to find the peer
		s.findNode(r.PeerID, callback)
		go func() {
			t := time.NewTimer(time.Second * 5)
			select {
			case n := <-callback:
				if n != node.EmptyNode { // we found it - now we can send the message to it
					s.localNode.Debug("Peer %s found... - registering and sending", r.PeerID)
					s.msgPendingRegister <- r
					s.RegisterNode(n)
				} else {
					s.localNode.Debug("Peer %s not found.... - can't send message, sending to queue", r.PeerID)
					s.retryMessage <- r
				}
			case <-t.C:
				s.retryMessage <- r
			}
		}()
		return
	}

	conn := peer.GetActiveConnection()

	if conn == nil {
		s.localNode.Warning("queuing protocol request until session is established...")
		// save the message for later sending and try to connect to the node
		s.messagesPendingSession[hex.EncodeToString(r.ReqID)] = r
		// try to connect to remote node and send the message once connected
		// todo: callback listener if connection fails (possibly after retries)
		s.onConnectionRequest(node.New(peer.PublicKey(), peer.TCPAddress()), nil)
		return
	}
	session := peer.GetAuthenticatedSession()
	if session == nil {
		if _, exist := s.messagesPendingSession[hex.EncodeToString(r.ReqID)]; !exist {
			s.messagesPendingSession[hex.EncodeToString(r.ReqID)] = r
		}
		return
	}

	encPayload, err := session.Encrypt(r.Payload)
	if err != nil {
		e := fmt.Errorf("aborting send - failed to encrypt payload: %v", err)
		go func() {
			if r.Callback != nil {
				r.Callback <- SendError{r.ReqID, e}
			}
		}()
		return
	}

	msg := &pb.CommonMessageData{
		SessionId: session.ID(),
		Payload:   encPayload,
		Timestamp: time.Now().Unix(),
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		e := fmt.Errorf("aborting send - invalid msg format %v", err)
		go func() {
			if r.Callback != nil {
				r.Callback <- SendError{r.ReqID, e}
			}
		}()
		return
	}

	// store callback by reqId for this connection so we can call back in case of msg timeout or other send failure
	if r.Callback != nil {
		callbacks := s.outgoingSendsCallbacks[conn.ID()]
		if callbacks == nil {
			s.outgoingSendsCallbacks[conn.ID()] = make(map[string]chan SendError)
		}

		s.outgoingSendsCallbacks[conn.ID()][hex.EncodeToString(r.ReqID)] = r.Callback
	}

	// finally - send it away!
	s.localNode.Debug("Sending protocol message down the connection to %v", log.PrettyID(r.PeerID))

	conn.Send(data, r.ReqID)
}

func (s *swarmImpl) onConnectionClosed(c net.Connection) {

	// forget about this connection
	id := c.ID()
	s.peerByConMapMutex.Lock()
	peer := s.peersByConnection[id]
	delete(s.connections, id)
	delete(s.peersByConnection, id)
	s.peerByConMapMutex.Unlock()
	if peer != nil {
		peer.UpdateConnection(id, nil)
	}
	s.sendNodeEvent(peer.String(), Disconnected)
}

func (s *swarmImpl) onRemoteClientConnected(c net.Connection) {
	// nop - a remote client connected - this is handled w message
	s.localNode.Debug("Remote client connected. %s", c.ID())
	peer := s.getPeerByConnection(c.ID())
	if peer != nil {
		s.sendNodeEvent(peer.String(), Connected)
	}
}

// Processes an incoming handshake protocol message
func (s *swarmImpl) onRemoteClientHandshakeMessage(msg net.IncomingMessage) {

	data := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Message, data)
	if err != nil {
		s.localNode.Warning("Unexpected handshake message format: %v", err)
		return
	}

	connID := msg.Connection.ID()

	// check if we already know about the remote node of this connection
	sender := s.getPeerByConnection(connID)
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

		sender.UpdateConnection(connID, msg.Connection)

		s.peerMapMutex.Lock()
		s.peers[sender.String()] = sender
		s.peerMapMutex.Unlock()

		s.peerByConMapMutex.Lock()
		s.peersByConnection[connID] = sender
		s.peerByConMapMutex.Unlock()

	}

	// update the routing table - we just heard from this node
	s.routingTable.Update(sender.GetRemoteNodeData())

	s.localNode.Debug("Routing message from %v to handshake handler ", sender.Pretty())
	// Let the demuxer route the message to its registered protocol handler
	s.demuxer.RouteIncomingMessage(NewIncomingMessage(sender, data.Protocol, msg.Message))
}

// Pre-process a protocol message from a remote client handling decryption and authentication
// Authenticated messages are forwarded to the demuxer for demuxing to protocol handlers
func (s *swarmImpl) onRemoteClientProtocolMessage(msg net.IncomingMessage, c *pb.CommonMessageData) {

	// Locate the session
	sid := hex.EncodeToString(c.SessionId)
	s.sessionsMutex.RLock()
	session := s.allSessions[sid]
	s.sessionsMutex.RUnlock()

	if session == nil || !session.IsAuthenticated() {
		s.localNode.Debug("Expected to have this session with this node")
		s.addIncomingPendingMessage(incomingPendingMessage{sid, msg})
		s.localNode.Debug("Stored incoming message as pending, try again when %s will establish", sid)
		return
	}
	s.peerMapMutex.RLock()
	remoteNode := s.peers[session.RemoteNodeID()]
	s.peerMapMutex.RUnlock()
	if remoteNode == nil {
		s.localNode.Warning("Dropping incoming protocol message - expected to have data about this node for an established session")
		return
	}

	decPayload, err := session.Decrypt(c.Payload)
	if err != nil {
		s.localNode.Warning("Dropping incoming protocol message - can't decrypt message payload with session key. %v", err)
		return
	}

	pm := &pb.ProtocolMessage{}
	err = proto.Unmarshal(decPayload, pm)
	if err != nil {
		s.localNode.Warning("Dropping incoming protocol message - Failed to get protocol message from payload. %v", err)
		return
	}

	// authenticate message author - we already authenticated the sender via the shared session key secret
	err = pm.AuthenticateAuthor()
	if err != nil {
		s.localNode.Warning("Dropping incoming protocol message - Failed to verify author. %v", err)
		return
	}

	s.localNode.Debug("Message %s author authenticated.", hex.EncodeToString(pm.GetMetadata().ReqId), pm.GetMetadata().Protocol)

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

	// check that the message was send within a reasonable time
	if ok := timesync.CheckMessageDrift(c.Timestamp); !ok {
		s.localNode.Error("Received out of sync msg from %s dropping .. ", msg.Connection.RemoteAddr())
		return
	}

	str := fmt.Sprintf("Incoming message to %v // ", s.localNode.Pretty())

	// route messages based on msg payload length
	if len(c.Payload) == 0 {
		// handshake messages have no enc payload
		s.localNode.Debug(fmt.Sprintf(str+"Type: Handshake, %v", hex.EncodeToString(c.SessionId)))
		s.onRemoteClientHandshakeMessage(msg)

	} else {
		s.localNode.Debug(fmt.Sprintf(str + "Type: ProtocolMessage"))
		// protocol messages are encrypted in payload
		s.onRemoteClientProtocolMessage(msg, c)
	}
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onMessageSentEvent(evt net.MessageSentEvent) {

	// message was written to a connection without an error
	callbacks := s.outgoingSendsCallbacks[evt.Connection.ID()]
	if callbacks == nil {
		return
	}

	reqID := hex.EncodeToString(evt.ID)
	callback := callbacks[reqID]
	if callback == nil {
		return
	}
	// TODO : Actually send to callback that message was sent.
	// stop tracking this outgoing message - it was sent
	delete(callbacks, reqID)
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onConnectionError(ce net.ConnectionError) {
	s.sendMessageSendError(ce)
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onMessageSendError(mse net.MessageSendError) {
	s.sendMessageSendError(net.ConnectionError{Connection: mse.Connection, Err: mse.Err, ID: mse.ID})
}

// send a msg send error back to the callback registered by reqID
func (s *swarmImpl) sendMessageSendError(cr net.ConnectionError) {

	c := cr.Connection
	callbacks := s.outgoingSendsCallbacks[c.ID()]

	if callbacks == nil {
		return
	}

	reqID := hex.EncodeToString(cr.ID)
	callback := callbacks[reqID]

	if callback == nil {
		return
	}

	// there will be no more callbacks for this reqID
	delete(callbacks, reqID)

	go func() {
		// TODO : make sure that goroutine doesn't leak. ( Select on timeout? )
		// TODO : Select on default. maybe genreic function to select and default.
		// send the error to the callback channel
		callback <- SendError{cr.ID, cr.Err}
	}()
}

// TODO : move peer store management out of swarm
func (s *swarmImpl) onGetPeerRequest(gpr getPeerRequest) {

	s.peerMapMutex.RLock()
	p, ok := s.peers[gpr.peerID]
	s.peerMapMutex.RUnlock()
	if !ok {
		go func() { gpr.callback <- nil }()
		return
	}

	go func() { gpr.callback <- p }()
}
