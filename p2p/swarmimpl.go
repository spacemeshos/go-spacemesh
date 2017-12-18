package p2p

import (
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/net"
)

type swarmImpl struct {

	// set in construction and immutable state
	network   net.Net
	localNode LocalNode
	demuxer   Demuxer

	// Internal state not thread safe state - must be access only from methods dispatched from the internal event handler

	peers             map[string]RemoteNode     // NodeId -> RemoteNode. known remote nodes. Swarm is a peer store.
	connections       map[string]net.Connection // ConnId -> Connection - all open connections for tracking and mngmnt
	peersByConnection map[string]RemoteNode     // ConnId -> RemoteNode remote nodes indexed by their connections.

	// todo: remove all idle sessions every n hours
	allSessions map[string]NetworkSession // SessionId -> Session data. all authenticated session

	messagesPendingSession map[string]SendMessageReq // k - unique req id. outgoing messages which pend an auth session with remote node to be sent out

	// comm channels
	connectionRequests chan RemoteNodeData // local requests to establish a session w a remote node
	disconnectRequests chan RemoteNodeData // local requests to kill session and disconnect from node

	sendMsgRequests  chan SendMessageReq // local request to send a session message to a node and callback on error or data
	sendHandshakeMsg chan SendMessageReq // local request to send a handshake protocol message

	kill chan bool // local request to kill the swamp from outside. e.g when local node is shutting down

	registerNodeReq chan RemoteNodeData // local request to register a node based on minimal data

	// swarm provides handshake protocol
	handshakeProtocol HandshakeProtocol

	// handshake protocol callback - sessions updates are pushed here
	newSessions chan HandshakeData // gets callback from handshake protocol when new session are created and/or auth

}

func NewSwarm(tcpAddress string, l LocalNode) (Swarm, error) {

	n, err := net.NewNet(tcpAddress, l.Config())
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
		connections:            make(map[string]net.Connection),
		messagesPendingSession: make(map[string]SendMessageReq),
		allSessions:            make(map[string]NetworkSession),

		connectionRequests: make(chan RemoteNodeData, 10),
		disconnectRequests: make(chan RemoteNodeData, 10),

		sendMsgRequests:  make(chan SendMessageReq, 20),
		sendHandshakeMsg: make(chan SendMessageReq, 20),
		demuxer:          NewDemuxer(),
		newSessions:      make(chan HandshakeData, 10),
		registerNodeReq:  make(chan RemoteNodeData, 10),
	}

	log.Info("Created swarm %s for local node %s", tcpAddress, l.String())

	s.handshakeProtocol = NewHandshakeProtocol(s)
	s.handshakeProtocol.RegisterNewSessionCallback(s.newSessions)

	go s.beginProcessingEvents()

	return s, err
}

func (s *swarmImpl) getHandshakeProtocol() HandshakeProtocol {
	return s.handshakeProtocol
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

func (s *swarmImpl) GetLocalNode() LocalNode {
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

func (s *swarmImpl) SendHandshakeMessage(req SendMessageReq) {
	s.sendHandshakeMsg <- req
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

		case r := <-s.sendHandshakeMsg:
			s.onSendHandshakeMessage(r)

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
