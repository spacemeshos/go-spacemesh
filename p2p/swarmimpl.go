package p2p

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/timesync"
)

type nodeEventCallbacks []NodeEventCallback

type swarmImpl struct {
	bootstrapped chan struct{}      // Channel to notify bootstrap loop to break
	afterBoot    chan chan struct{} // Channel to block outside and wait for node to bootstrap
	// set in construction and immutable state
	network   net.Net
	localNode LocalNode
	demuxer   Demuxer

	config nodeconfig.SwarmConfig

	nodeEventRegChannel    chan NodeEventCallback
	nodeEventRemoveChannel chan NodeEventCallback
	// node state callbacks
	nec nodeEventCallbacks

	// Internal state is not thread safe - must be accessed only from methods dispatched from the internal event handler
	peers map[string]Peer // NodeID -> Peer. known remote nodes. Swarm is a peer store.

	getPeerRequests   chan getPeerRequest
	connections       map[string]net.Connection // ConnID -> Connection - all open connections for tracking and mngmnt
	peersByConnection map[string]Peer           // ConnID -> Peer remote nodes indexed by their connections.

	// todo: remove all idle sessions every n hours (configurable)
	allSessions map[string]NetworkSession // SessionId -> Session data. all authenticated session

	outgoingSendsCallbacks map[string]map[string]chan SendError // k - conn id v-  reqID -> chan SendError

	messagesPendingSession map[string]SendMessageReq // k - unique req id. outgoing messages which pend an auth session with remote node to be sent out

	incomingPendingSession  map[string][]net.IncomingMessage
	incomingPendingMessages chan incomingPendingMessage

	msgPendingRegister          chan SendMessageReq
	messagesPendingRegistration map[string]SendMessageReq // Messages waiting for peers to register.

	retryMessage       chan SendMessageReq
	messagesRetryQueue []SendMessageReq

	// comm channels
	connectionRequests chan nodeAction          // local requests to establish a session w a remote node
	disconnectRequests chan node.RemoteNodeData // local requests to kill session and disconnect from node

	sendMsgRequests  chan SendMessageReq // local request to send a session message to a node and callback on error or data
	sendHandshakeMsg chan SendMessageReq // local request to send a handshake protocol message

	shutdown chan struct{} // local request to kill the swarm from outside. e.g when local node is shutting down

	registerNodeReq chan node.RemoteNodeData // local request to register a node based on minimal data

	// swarm provides handshake protocol
	handshakeProtocol HandshakeProtocol

	// handshake protocol callback - sessions updates are pushed here
	newSessions chan HandshakeData // gets callback from handshake protocol when new session are created and/or auth

	// dht find-node protocol
	findNodeProtocol FindNodeProtocol

	routingTable table.RoutingTable
}

// NewSwarm creates a new swarm for a local node.
func NewSwarm(tcpAddress string, l LocalNode) (Swarm, error) {

	n, err := net.NewNet(tcpAddress, l.Config(), l.GetLogger())
	if err != nil {
		log.Error("can't create swarm without a network", err)
		return nil, err
	}

	s := &swarmImpl{
		bootstrapped: make(chan struct{}),
		afterBoot:    make(chan chan struct{}, 1),
		localNode:    l,
		network:      n,
		shutdown:     make(chan struct{}), // non-buffered so requests to shutdown block until swarm is shut down

		nodeEventRegChannel: make(chan NodeEventCallback, 1),
		nec:                 make(nodeEventCallbacks, 0),

		peersByConnection:      make(map[string]Peer),
		peers:                  make(map[string]Peer),
		getPeerRequests:        make(chan getPeerRequest),
		connections:            make(map[string]net.Connection),
		messagesPendingSession: make(map[string]SendMessageReq),

		incomingPendingSession:  make(map[string][]net.IncomingMessage),
		incomingPendingMessages: make(chan incomingPendingMessage),

		messagesPendingRegistration: make(map[string]SendMessageReq),
		msgPendingRegister:          make(chan SendMessageReq, 10),

		retryMessage:       make(chan SendMessageReq, 10),
		messagesRetryQueue: make([]SendMessageReq, 0),

		allSessions: make(map[string]NetworkSession),

		outgoingSendsCallbacks: make(map[string]map[string]chan SendError),

		// todo: figure optimal buffer size for chans
		// ref: https://www.goinggo.net/2017/10/the-behavior-of-channels.html

		connectionRequests: make(chan nodeAction, 20),
		disconnectRequests: make(chan node.RemoteNodeData, 20),

		sendMsgRequests:  make(chan SendMessageReq, 20),
		sendHandshakeMsg: make(chan SendMessageReq, 20),
		demuxer:          NewDemuxer(l.GetLogger()),
		newSessions:      make(chan HandshakeData, 20),
		registerNodeReq:  make(chan node.RemoteNodeData, 20),

		config: l.Config().SwarmConfig,
	}

	// node's routing table
	s.routingTable = table.NewRoutingTable(int(s.config.RoutingTableBucketSize), l.DhtID(), l.GetLogger())

	// findNode dht protocol
	s.findNodeProtocol = NewFindNodeProtocol(s)

	s.localNode.Debug("Created swarm for local node %s, %s", tcpAddress, l.Pretty())

	s.handshakeProtocol = NewHandshakeProtocol(s)
	s.handshakeProtocol.RegisterNewSessionCallback(s.newSessions)

	go s.beginProcessingEvents(s.config.Bootstrap)

	return s, err
}

func (s *swarmImpl) bootComplete() {
	s.bootstrapped <- struct{}{}
}

func (s *swarmImpl) AfterBootstrap(q chan struct{}) {
	s.afterBoot <- q
}

// Register an event handler callback for connection state events
func (s *swarmImpl) RegisterNodeEventsCallback(callback NodeEventCallback) {
	s.nodeEventRegChannel <- callback
}

func (s *swarmImpl) RemoveNodeEventsCallback(callback NodeEventCallback) {
	s.nodeEventRemoveChannel <- callback
}

func (s *swarmImpl) registerNodeEventsCallback(callback NodeEventCallback) {
	s.nec = append(s.nec, callback)
}

func (s *swarmImpl) removeNodeEventsCallback(callback NodeEventCallback) {
	for i, c := range s.nec {
		if c == callback {
			s.nec = append(s.nec[:i], s.nec[i+1:]...)
			return
		}
	}
}

// Register an event handler callback for connection state events
func (s *swarmImpl) RetryMessageLater(req SendMessageReq) {
	s.retryMessage <- req
}

func (s *swarmImpl) retryMessageLater(req SendMessageReq) {
	s.messagesRetryQueue = append(s.messagesRetryQueue, req)
}

func (s *swarmImpl) resendRetryMessages() {
	for i, msg := range s.messagesRetryQueue {
		s.messagesRetryQueue = append(s.messagesRetryQueue[:i], s.messagesRetryQueue[i+1:]...)
		go func(msg2 SendMessageReq, t int) {
			time.Sleep((1 * time.Second) * time.Duration(t))
			s.SendMessage(msg2)
		}(msg, i)
	}
}

// Sends a connection event to all registered clients
func (s *swarmImpl) sendNodeEvent(peerID string, state NodeState) {

	s.localNode.Debug(">> Node event for <%s>. State: %+v", peerID[:6], state)

	evt := NodeEvent{peerID, state}
	for _, c := range s.nec {
		go func(c NodeEventCallback) { c <- evt }(c)
	}
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

func (s *swarmImpl) ConnectTo(req node.RemoteNodeData, done chan error) {
	s.connectionRequests <- nodeAction{req, done}
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

func (s *swarmImpl) GetPeer(id string, callback chan Peer) {
	gpr := getPeerRequest{
		peerID:   id,
		callback: callback,
	}
	s.getPeerRequests <- gpr
}

// Sends a message to a remote node
// Swarm will establish session if needed or use an existing session and open connection
// Designed to be used by any high level protocol
// req.reqID: globally unique id string - used for tracking messages we didn't get a response for yet
// req.msg: marshaled message data
// req.destId: receiver remote node public key/id
func (s *swarmImpl) SendMessage(req SendMessageReq) {
	s.sendMsgRequests <- req
}

func (s *swarmImpl) sendHandshakeMessage(req SendMessageReq) {
	s.sendHandshakeMsg <- req
}

func (s *swarmImpl) Shutdown() {
	s.shutdown <- struct{}{}
}

func (s *swarmImpl) shutDownInternal() {

	// close all open connections
	for _, c := range s.connections {
		c.Close()
	}

	for _, p := range s.peers {
		p.DeleteAllConnections()
	}

	// shutdown network
	s.network.Shutdown()
}

// Swarm serial event processing
// provides concurrency safety as only one callback is executed at a time
// so there's no need for sync internal data structures
func (s *swarmImpl) beginProcessingEvents(bootstrap bool) {
	checkTimeSync := time.NewTicker(nodeconfig.TimeConfigValues.RefreshNtpInterval)
	retryMessage := time.NewTicker(time.Second * 5)

	if bootstrap {
		s.bootstrapLoop(retryMessage)
	}

Loop:
	for {
		select {
		case <-s.shutdown:
			s.shutDownInternal()
			break Loop
		case q := <-s.afterBoot: // if we're here we have done bootstrap already.
			q <- struct{}{}
		case r := <-s.sendHandshakeMsg:
			s.onSendHandshakeMessage(r)

		case session := <-s.newSessions:
			s.onNewSession(session)

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

		case n := <-s.connectionRequests:
			s.onConnectionRequest(n.req, n.done)

		case n := <-s.disconnectRequests:
			s.onDisconnectionRequest(n)

		case d := <-s.registerNodeReq:
			s.onRegisterNodeRequest(d)

		case c := <-s.nodeEventRegChannel:
			s.registerNodeEventsCallback(c)

		case c := <-s.nodeEventRemoveChannel:
			s.removeNodeEventsCallback(c)

		case mpr := <-s.msgPendingRegister:
			s.addMessagePendingRegistration(mpr)

		case ipm := <-s.incomingPendingMessages:
			s.addIncomingPendingMessage(ipm)

		case gpr := <-s.getPeerRequests:
			s.onGetPeerRequest(gpr)

		case qmsg := <-s.retryMessage:
			s.retryMessageLater(qmsg)

		case <-retryMessage.C:
			s.resendRetryMessages()

		case <-checkTimeSync.C:
			_, err := timesync.CheckSystemClockDrift()
			if err != nil {
				checkTimeSync.Stop()
				s.localNode.Error("System time could'nt synchronize %s", err)
				go s.localNode.Shutdown()
			}
		}
	}
}
