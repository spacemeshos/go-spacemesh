package p2p

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"time"

	//"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/timesync"
)

type nodeEventCallbacks []NodeEventCallback

type incomingPendingMessage struct {
	SessionID string
	Message   net.IncomingMessage
}

type getPeerRequest struct {
	peerID   string
	callback chan Peer
}

type swarmImpl struct {
	bootstrapSignal chan struct{}
	// set in construction and immutable state
	network   net.Net
	localNode LocalNode
	demuxer   Demuxer

	config nodeconfig.SwarmConfig

	nodeEventRegChannel chan NodeEventCallback
	// node state callbacks
	nec nodeEventCallbacks

	// Internal state is not thread safe - must be accessed only from methods dispatched from the internal event handler
	peers             map[string]Peer // NodeID -> Peer. known remote nodes. Swarm is a peer store.
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

	// comm channels
	connectionRequests chan node.RemoteNodeData // local requests to establish a session w a remote node
	disconnectRequests chan node.RemoteNodeData // local requests to kill session and disconnect from node

	sendMsgRequests  chan SendMessageReq // local request to send a session message to a node and callback on error or data
	sendHandshakeMsg chan SendMessageReq // local request to send a handshake protocol message

	shutdown chan bool // local request to kill the swarm from outside. e.g when local node is shutting down

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
		localNode: l,
		network:   n,
		shutdown:  make(chan bool), // non-buffered so requests to shutdown block until swarm is shut down

		bootstrapSignal: make(chan struct{}),

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
		msgPendingRegister:          make(chan SendMessageReq),

		allSessions: make(map[string]NetworkSession),

		outgoingSendsCallbacks: make(map[string]map[string]chan SendError),

		// todo: figure optimal buffer size for chans
		// ref: https://www.goinggo.net/2017/10/the-behavior-of-channels.html

		connectionRequests: make(chan node.RemoteNodeData, 20),
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

	s.localNode.Info("Created swarm for local node %s, %s", tcpAddress, l.Pretty())

	s.handshakeProtocol = NewHandshakeProtocol(s)
	s.handshakeProtocol.RegisterNewSessionCallback(s.newSessions)

	go s.beginProcessingEvents()

	if s.config.Bootstrap {
		s.Bootstrap()
	}

	return s, err
}

// Register an event handler callback for connection state events
func (s *swarmImpl) RegisterNodeEventsCallback(callback NodeEventCallback) {
	s.nodeEventRegChannel <- callback
}

func (s *swarmImpl) registerNodeEventsCallback(callback NodeEventCallback) {
	s.nec = append(s.nec, callback)
}

// Sends a connection event to all registered clients
func (s *swarmImpl) sendNodeEvent(peerID string, state NodeState) {

	s.localNode.GetLogger().Debug(">> Node event for <%s>. State: %+v Sending events to %d", peerID[:6], state, len(s.nec))
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

func (s *swarmImpl) GetPeer(id string, callback chan Peer) {
	gpr := getPeerRequest{
		peerID:   id,
		callback: callback,
	}
	s.getPeerRequests <- gpr
}

// Starts the bootstrap process
// Query the bootstrap nodes we have with our own nodeID in order to fill our routing table with close nodes.
func (s *swarmImpl) Bootstrap() {
	s.bootstrapSignal <- struct{}{}
}

func (s *swarmImpl) bootstrap() {
	s.localNode.Info("Starting bootstrap...")

	// register bootstrap nodes
	bn := 0
	for _, n := range s.config.BootstrapNodes {
		rn := node.NewRemoteNodeDataFromString(n)
		if s.localNode.String() != rn.ID() {
			s.onRegisterNodeRequest(rn)
			bn++
		}
	}

	c := int(s.config.RandomConnections)
	if c <= 0 {
		log.Warning("0 random connections - aborting bootstrap")
		return
	}

	if bn > 0 {

		// isseus findNode requests until DHT has at least c peers and start a periodic refresh func
		errSignal := make(chan error)
		go s.routingTable.Bootstrap(s.findNode, s.localNode.String(), c, errSignal)

		go func(errchan chan error) {
			err := <-errchan

			if err != nil {
				s.localNode.Error("Bootstrapping the node failed ", err)
				return
			}

			//TODO : Fix connecting to random connection
			s.ConnectToRandomNodes(c)
		}(errSignal)

		return
	}
}

// Connect up to count random nodes
func (s *swarmImpl) ConnectToRandomNodes(count int) {

	s.localNode.Info("Attempting to connect to %d random nodes...", count)

	// create callback to receive result
	c1 := make(table.PeersOpChannel)

	// find nearest peers
	timeout := time.NewTimer(time.Second * 90)
	s.routingTable.NearestPeers(table.NearestPeersReq{ID: s.localNode.DhtID(), Count: count, Callback: c1})

	select {
	case c := <-c1:
		if len(c.Peers) == 0 {
			s.GetLocalNode().Warning("Did not find any random nodes close to self")
		}

		for _, p := range c.Peers {
			s.ConnectTo(p)
		}

	case <-timeout.C:
		s.localNode.Error("timeout getting random nearby nodes")
	}

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
	s.shutdown <- true
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
func (s *swarmImpl) beginProcessingEvents() {
	checkTimeSync := time.NewTicker(nodeconfig.TimeConfigValues.RefreshNtpInterval.Duration())
Loop:
	for {
		select {
		case <-s.bootstrapSignal:
			s.bootstrap()
		case <-s.shutdown:
			s.shutDownInternal()
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

		case c := <-s.nodeEventRegChannel:
			s.registerNodeEventsCallback(c)

		case mpr := <-s.msgPendingRegister:
			s.addMessagePendingRegistration(mpr)

		case ipm := <-s.incomingPendingMessages:
			s.addIncomingPendingMessage(ipm)

		case gpr := <-s.getPeerRequests:
			s.onGetPeerRequest(gpr)

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
