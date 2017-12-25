package p2p

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"

	//"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
)

type swarmImpl struct {

	// set in construction and immutable state
	network   net.Net
	localNode LocalNode
	demuxer   Demuxer

	config nodeconfig.SwarmConfig

	// Internal state not thread safe state - must be access only from methods dispatched from the internal event handler

	peers             map[string]Peer           // NodeId -> Peer. known remote nodes. Swarm is a peer store.
	connections       map[string]net.Connection // ConnId -> Connection - all open connections for tracking and mngmnt
	peersByConnection map[string]Peer           // ConnId -> Peer remote nodes indexed by their connections.

	// todo: remove all idle sessions every n hours
	allSessions map[string]NetworkSession // SessionId -> Session data. all authenticated session

	outgoingSendsCallbacks map[string]map[string]chan SendError // k - conn id v-  reqId -> chan SendError

	messagesPendingSession map[string]SendMessageReq // k - unique req id. outgoing messages which pend an auth session with remote node to be sent out

	// comm channels
	connectionRequests chan node.RemoteNodeData // local requests to establish a session w a remote node
	disconnectRequests chan node.RemoteNodeData // local requests to kill session and disconnect from node

	sendMsgRequests  chan SendMessageReq // local request to send a session message to a node and callback on error or data
	sendHandshakeMsg chan SendMessageReq // local request to send a handshake protocol message

	kill chan bool // local request to kill the swamp from outside. e.g when local node is shutting down

	registerNodeReq chan node.RemoteNodeData // local request to register a node based on minimal data

	// swarm provides handshake protocol
	handshakeProtocol HandshakeProtocol

	// handshake protocol callback - sessions updates are pushed here
	newSessions chan HandshakeData // gets callback from handshake protocol when new session are created and/or auth

	// dht find-node protocol
	findNodeProtocol FindNodeProtocol

	routingTable table.RoutingTable
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

		peersByConnection:      make(map[string]Peer),
		peers:                  make(map[string]Peer),
		connections:            make(map[string]net.Connection),
		messagesPendingSession: make(map[string]SendMessageReq),
		allSessions:            make(map[string]NetworkSession),

		outgoingSendsCallbacks: make(map[string]map[string]chan SendError),

		connectionRequests: make(chan node.RemoteNodeData, 10),
		disconnectRequests: make(chan node.RemoteNodeData, 10),

		sendMsgRequests:  make(chan SendMessageReq, 20),
		sendHandshakeMsg: make(chan SendMessageReq, 20),
		demuxer:          NewDemuxer(),
		newSessions:      make(chan HandshakeData, 10),
		registerNodeReq:  make(chan node.RemoteNodeData, 10),

		config: l.Config().SwarmConfig,
	}

	// nodes routing table
	s.routingTable = table.NewRoutingTable(int(s.config.RoutingTableBucketSize), l.DhtId())

	// findNode dht protocol
	s.findNodeProtocol = NewFindNodeProtocol(s)

	log.Info("Created swarm %s for local node %s", tcpAddress, l.String())

	s.handshakeProtocol = NewHandshakeProtocol(s)
	s.handshakeProtocol.RegisterNewSessionCallback(s.newSessions)

	go s.beginProcessingEvents()

	if s.config.Bootstrap {
		s.bootstrap()
	}

	return s, err
}

func (s *swarmImpl) getFindNodeProtocol() FindNodeProtocol {
	return s.findNodeProtocol
}

func (s *swarmImpl) getHandshakeProtocol() HandshakeProtocol {
	return s.handshakeProtocol
}

func (s *swarmImpl) getRoutingTable() table.RoutingTable {
	return s.routingTable
}

// register a node with the swarm
func (s *swarmImpl) RegisterNode(data node.RemoteNodeData) {
	s.registerNodeReq <- data
}

func (s *swarmImpl) ConnectTo(req node.RemoteNodeData) {
	s.connectionRequests <- req
}

func (s *swarmImpl) DisconnectFrom(req node.RemoteNodeData) {
	s.disconnectRequests <- req
}

func (s *swarmImpl) GetDemuxer() Demuxer {
	return s.demuxer
}

func (s *swarmImpl) GetLocalNode() LocalNode {
	return s.localNode
}

// start the boostrap process
func (s *swarmImpl) bootstrap() {

	// register bootstrap nodes
	bn := 0
	for _, n := range s.config.BootstrapNodes {
		rn := node.NewRemoteNodeDataFromString(n)
		if s.localNode.String() != rn.Id() {
			s.onRegisterNodeRequest(rn)
			bn += 1
		}
	}

	c := int(s.config.RandomConnections)
	if c == 0 {
		return
	}

	if bn > 0 {
		// if we know about at least one bootstrap node then attempt to connect to c random nodes
		go s.ConnectToRandomNodes(c, nil)
	}
}

// Connect up to count random nodes
func (s *swarmImpl) ConnectToRandomNodes(count int, callback chan node.RemoteNodeData) {
	c := make(chan node.RemoteNodeData, count)
	s.findNode(s.localNode.String(), callback)
	select {
	case n := <-c:
		if n != nil {
			s.ConnectTo(n)
			if callback != nil {
				go func() { callback <- n }()
			}
		}
	}
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

func (s *swarmImpl) sendHandshakeMessage(req SendMessageReq) {
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

		case evt := <-s.network.GetMessageSentCallback():
			s.onMessageSentEvent(evt)

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
