package p2p

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/discovery"
	"github.com/spacemeshos/go-spacemesh/p2p/gossip"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"strings"

	inet "net"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectingTimeout is the timeout we wait when trying to connect a neighborhood
const ConnectingTimeout = 20 * time.Second //todo: add to the config

type cPool interface {
	GetConnection(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error)
	GetConnectionIfExists(pk p2pcrypto.PublicKey) (net.Connection, error)
	CloseConnection(key p2pcrypto.PublicKey)
	Shutdown()
}

type swarm struct {
	// init info props
	started   uint32
	bootErr   error
	bootChan  chan struct{}
	gossipErr error
	gossipC   chan struct{}

	config config.Config
	logger log.Log
	// Context for cancel
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Shutdown the loop
	shutdownOnce sync.Once
	shutdown     chan struct{} // local request to kill the swarm from outside. e.g when local node is shutting down

	// set in construction and immutable state
	lNode node.LocalNode

	// map between protocol names to listening protocol handlers
	// NOTE: maybe let more than one handler register on a protocol ?
	directProtocolHandlers map[string]chan service.DirectMessage
	gossipProtocolHandlers map[string]chan service.GossipMessage
	protocolHandlerMutex   sync.RWMutex

	network    *net.Net    // (tcp) networking service
	udpnetwork *net.UDPNet // (udp) networking service

	cPool  cPool // conenction cache
	gossip *gossip.Protocol

	discover  discovery.PeerStore // peer addresses store
	udpServer *UDPMux             // protocol switch that includes a udp networking service

	//neighborhood
	initOnce sync.Once
	initial  chan struct{}

	outpeersMutex sync.RWMutex
	inpeersMutex  sync.RWMutex
	outpeers      map[p2pcrypto.PublicKey]struct{}
	inpeers       map[p2pcrypto.PublicKey]struct{}

	morePeersReq      chan struct{}
	connectingTimeout time.Duration

	peerLock   sync.RWMutex
	newPeerSub []chan p2pcrypto.PublicKey
	delPeerSub []chan p2pcrypto.PublicKey
}

func (s *swarm) waitForBoot() error {
	_, ok := <-s.bootChan
	if !ok {
		return s.bootErr
	}
	return nil
}

func (s *swarm) waitForGossip() error {
	_, ok := <-s.gossipC
	if !ok {
		return s.gossipErr
	}
	return nil
}

// newSwarm creates a new P2P instance, configured by config, if newNode is true it will create a new node identity
// and not load from disk. it creates a new `net`, connection pool and discovery.
func newSwarm(ctx context.Context, config config.Config, logger log.Log, datadir string) (*swarm, error) {

	port := config.TCPPort
	tcpaddr := &inet.TCPAddr{inet.ParseIP("0.0.0.0"), port, ""}
	udpaddr := &inet.UDPAddr{inet.ParseIP("0.0.0.0"), port, ""}

	var l node.LocalNode
	var err error

	// Load an existing identity from file if exists.
	if config.NodeID != "" {
		l, err = node.LoadIdentity(datadir, config.NodeID)
	} else {
		l, err = node.ReadFirstNodeData(datadir)
		if err != nil {
			logger.Warning("failed to load p2p identity from disk at %v. creating a new identity, err:%v", datadir, err)
			l, err = node.NewNodeIdentity()
		}
	}

	if err != nil {
		return nil, err
	}

	logger.Info("Local node identity >> %v", l.PublicKey().String())
	logger = logger.WithFields(log.String("P2PID", l.PublicKey().String()))

	if datadir != "" {
		err := l.PersistData(datadir)
		if err != nil {
			logger.Warning("failed to persist p2p node data err=%v", err)
		}
	}

	n, err := net.NewNet(config, l, tcpaddr, logger.WithName("tcpnet"))
	if err != nil {
		return nil, fmt.Errorf("can't create swarm without a network, err: %v", err)
	}

	udpnet, err := net.NewUDPNet(config, l, udpaddr, logger.WithName("udpnet"))
	if err != nil {
		return nil, err
	}

	localCtx, cancel := context.WithCancel(ctx)

	s := &swarm{
		ctx:        localCtx,
		cancelFunc: cancel,

		config: config,
		logger: logger,

		lNode: l,

		bootChan: make(chan struct{}),
		gossipC:  make(chan struct{}),
		shutdown: make(chan struct{}), // non-buffered so requests to shutdown block until swarm is shut down

		initial:           make(chan struct{}),
		morePeersReq:      make(chan struct{}, config.MaxInboundPeers+config.OutboundPeersTarget),
		inpeers:           make(map[p2pcrypto.PublicKey]struct{}),
		outpeers:          make(map[p2pcrypto.PublicKey]struct{}),
		newPeerSub:        make([]chan p2pcrypto.PublicKey, 0, 10),
		delPeerSub:        make([]chan p2pcrypto.PublicKey, 0, 10),
		connectingTimeout: ConnectingTimeout,

		directProtocolHandlers: make(map[string]chan service.DirectMessage),
		gossipProtocolHandlers: make(map[string]chan service.GossipMessage),

		network:    n,
		udpnetwork: udpnet,
	}

	mux := NewUDPMux(s.lNode, s.lookupFunc, udpnet, s.config.NetworkID, s.logger)
	s.udpServer = mux

	// todo : if discovery on
	s.discover = discovery.New(l, config.SwarmConfig, s.udpServer, datadir, s.logger) // create table and discovery protocol

	cpool := connectionpool.NewConnectionPool(s.network.Dial, l.PublicKey(), logger)

	s.network.SubscribeOnNewRemoteConnections(func(nce net.NewConnectionEvent) {
		err := cpool.OnNewConnection(nce)
		if err != nil {
			s.logger.Warning("adding incoming connection err=", err)
			// no need to continue since this means connection already exists.
			return
		}
		s.onNewConnection(nce)
	})
	s.network.SubscribeClosingConnections(cpool.OnClosedConnection)
	s.network.SubscribeClosingConnections(s.onClosedConnection)

	s.cPool = cpool

	s.gossip = gossip.NewProtocol(config.SwarmConfig, s, s.LocalNode().PublicKey(), s.logger)

	s.logger.Debug("Created newSwarm with key %s", l.PublicKey())
	return s, nil
}

func (s *swarm) lookupFunc(target p2pcrypto.PublicKey) (*node.NodeInfo, error) {
	return s.discover.Lookup(target)
}

func (s *swarm) onNewConnection(nce net.NewConnectionEvent) {
	// todo: consider doing cpool actions from here instead of registering cpool as well.
	err := s.addIncomingPeer(nce.Node.PublicKey())
	if err != nil {
		s.logger.Warning("Error adding new connection %v, err: %v", nce.Node.PublicKey(), err)
		// todo: send rejection reason
		s.cPool.CloseConnection(nce.Node.PublicKey())
	}
}

func (s *swarm) onClosedConnection(cwe net.ConnectionWithErr) {
	// we don't want to block, we know this node's connection was closed.
	// todo: pass on closing reason, if we closed the connection.
	// 	mark address book or ban if malicious activity recognised
	s.Disconnect(cwe.Conn.RemotePublicKey())
}

// Start starts the p2p service. if configured, bootstrap is started in the background.
// returns error if the swarm is already running or there was an error starting one of the needed services.
func (s *swarm) Start() error {
	if atomic.LoadUint32(&s.started) == 1 {
		return errors.New("swarm already running")
	}

	var err error

	atomic.StoreUint32(&s.started, 1)
	s.logger.Debug("Starting the p2p layer")

	err = s.network.Start()
	if err != nil {
		s.logger.Error("Error starting network services err=", err)
		return err
	}

	if err := s.udpnetwork.Start(); err != nil {
		return err
	}

	tcpAddress := s.network.LocalAddr().(*inet.TCPAddr)
	udpAddress := s.udpnetwork.LocalAddr().(*inet.UDPAddr)

	s.discover.SetLocalAddresses(tcpAddress.Port, udpAddress.Port) // todo: pass net.Addr and convert in discovery

	err = s.udpServer.Start()
	if err != nil {
		return err
	}

	s.logger.Debug("Starting to listen for network messages")
	s.listenToNetworkMessages() // fires up a goroutine for each queue of messages
	s.logger.Debug("starting the udp server")

	// TODO : insert new addresses to discovery

	if s.config.SwarmConfig.Bootstrap {
		go func() {
			b := time.Now()
			err := s.discover.Bootstrap(s.ctx)
			if err != nil {
				s.bootErr = err
				close(s.bootChan)
				s.Shutdown()
				return
			}
			close(s.bootChan)
			size := s.discover.Size()
			s.logger.Event().Info("discovery_bootstrap", log.Bool("success", size >= s.config.SwarmConfig.RandomConnections && s.bootErr == nil),
				log.Int("size", size), log.Duration("time_elapsed", time.Since(b)))
		}()
	}

	// todo: maybe reset peers that connected us while bootstrapping
	// wait for neighborhood
	if s.config.SwarmConfig.Gossip {
		// start gossip before starting to collect peers
		s.gossip.Start()
		go func() {
			if s.config.SwarmConfig.Bootstrap {
				if err := s.waitForBoot(); err != nil {
					return
				}
			}
			err := s.startNeighborhood() // non blocking
			//todo:maybe start listening only after we got enough outbound neighbors?
			if err != nil {
				s.gossipErr = err
				close(s.gossipC)
				s.Shutdown()
				return
			}
			<-s.initial
			close(s.gossipC)
		}() // todo handle error async
	}

	return nil
}

func (s *swarm) LocalNode() node.LocalNode {
	return s.lNode
}

func (s *swarm) connectionPool() cPool {
	return s.cPool
}

func (s *swarm) SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error {
	return s.sendMessageImpl(nodeID, protocol, payload)
}

func (s *swarm) SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	return s.sendMessageImpl(peerPubkey, protocol, service.DataBytes{Payload: payload})
}

// SendMessage Sends a message to a remote node
// swarm will establish session if needed or use an existing session and open connection
// Designed to be used by any high level protocol
// req.reqID: globally unique id string - used for tracking messages we didn't get a response for yet
// req.msg: marshaled message data
// req.destId: receiver remote node public key/id
// Local request to send a message to a remote node
func (s *swarm) sendMessageImpl(peerPubKey p2pcrypto.PublicKey, protocol string, payload service.Data) error {
	var err error
	var conn net.Connection

	if s.discover.IsLocalAddress(&node.NodeInfo{ID: peerPubKey.Array()}) {
		return errors.New("can't sent message to self")
		//TODO: if this is our neighbor it should be removed right now.
	}

	conn, err = s.cPool.GetConnectionIfExists(peerPubKey)

	if err != nil {
		return errors.New("this peers isn't a neighbor or lost connection")
	}

	session := conn.Session()
	if session == nil {
		s.logger.Warning("failed to send message to %v, no valid session.", conn.RemotePublicKey().String())
		return ErrNoSession
	}

	protomessage := &ProtocolMessage{
		Metadata: &ProtocolMessageMetadata{NextProtocol: protocol, ClientVersion: config.ClientVersion,
			Timestamp: time.Now().Unix(), AuthPubkey: s.LocalNode().PublicKey().Bytes()},
		Payload: nil,
	}

	realpayload, err := CreatePayload(payload)
	if err != nil {
		return err
	}

	protomessage.Payload = realpayload

	data, err := types.InterfaceToBytes(protomessage)
	if err != nil {
		return fmt.Errorf("failed to encode signed message err: %v", err)
	}

	final := session.SealMessage(data)

	if final == nil {
		return errors.New("encryption failed")
	}

	err = conn.Send(final)

	s.logger.Debug("DirectMessage sent successfully")

	return err
}

// RegisterDirectProtocol registers an handler for direct messaging based `protocol`
func (s *swarm) RegisterDirectProtocol(protocol string) chan service.DirectMessage { // TODO: not used - remove
	mchan := make(chan service.DirectMessage, s.config.BufferSize)
	s.protocolHandlerMutex.Lock()
	s.directProtocolHandlers[protocol] = mchan
	s.protocolHandlerMutex.Unlock()
	return mchan
}

// RegisterGossipProtocol registers an handler for gossip based `protocol`
func (s *swarm) RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage {
	mchan := make(chan service.GossipMessage, s.config.BufferSize)
	s.protocolHandlerMutex.Lock()
	s.gossip.SetPriority(protocol, prio)
	s.gossipProtocolHandlers[protocol] = mchan
	s.protocolHandlerMutex.Unlock()
	return mchan
}

// Shutdown sends a shutdown signal to all running services of swarm and then runs an internal shutdown to cleanup.
func (s *swarm) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.cancelFunc()
		close(s.shutdown)
		s.gossip.Close()
		s.discover.Shutdown()
		s.cPool.Shutdown()
		s.network.Shutdown()
		s.udpServer.Shutdown()

		s.protocolHandlerMutex.Lock()
		for i, _ := range s.directProtocolHandlers {
			delete(s.directProtocolHandlers, i)
			//close(prt) //todo: signal protocols to shutdown with closing chan. (this makes us send on closed chan. )
		}
		for i, _ := range s.gossipProtocolHandlers {
			delete(s.gossipProtocolHandlers, i)
			//close(prt) //todo: signal protocols to shutdown with closing chan. (this makes us send on closed chan. )
		}
		s.protocolHandlerMutex.Unlock()

		s.peerLock.Lock()
		for _, ch := range s.newPeerSub {
			close(ch)
		}
		for _, ch := range s.delPeerSub {
			close(ch)
		}
		s.delPeerSub = nil
		s.newPeerSub = nil
		s.peerLock.Unlock()
	})
}

// process an incoming message
func (s *swarm) processMessage(ime net.IncomingMessageEvent) {
	if s.config.MsgSizeLimit != config.UnlimitedMsgSize && len(ime.Message) > s.config.MsgSizeLimit {
		s.logger.With().Error("processMessage: message is too big",
			log.Int("limit", s.config.MsgSizeLimit), log.Int("actual", len(ime.Message)))
		return
	}

	err := s.onRemoteClientMessage(ime)
	if err != nil {
		s.logger.Error("Err reading message from %v, closing connection err=%v", ime.Conn.RemotePublicKey(), err)
		ime.Conn.Close()
		// TODO: differentiate action on errors
	}
}

// RegisterProtocolWithChannel configures and returns a channel for a given protocol.
func (s *swarm) RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage {
	s.protocolHandlerMutex.Lock()
	s.directProtocolHandlers[protocol] = ingressChannel
	s.protocolHandlerMutex.Unlock()
	return ingressChannel
}

// listenToNetworkMessages is waiting for network events from net as new connections or messages and handles them.
func (s *swarm) listenToNetworkMessages() {

	// We listen to each of the messages queues we get from `net
	// It's net's responsibility to distribute the messages to the queues
	// in a way that they're processing order will work
	// swarm process all the queues concurrently but synchronously for each queue

	netqueues := s.network.IncomingMessages()
	for nq := range netqueues { // run a separate worker for each queue.
		go func(c chan net.IncomingMessageEvent) {
			for {
				select {
				case msg := <-c:
					s.processMessage(msg)
				case <-s.shutdown:
					return
				}
			}
		}(netqueues[nq])
	}

}

// onRemoteClientMessage possible errors

var (
	// ErrBadFormat1 could'nt deserialize the payload
	ErrBadFormat1 = errors.New("bad msg format, could'nt deserialize 1")
	// ErrBadFormat2 could'nt deserialize the protocol message payload
	ErrBadFormat2 = errors.New("bad msg format, could'nt deserialize 2")
	// ErrOutOfSync is returned when messsage timestamp was out of sync
	ErrOutOfSync = errors.New("received out of sync msg")
	// ErrFailDecrypt session cant decrypt
	ErrFailDecrypt = errors.New("can't decrypt message payload with session key")
	// ErrNoProtocol we don't have the protocol message
	ErrNoProtocol = errors.New("received msg to an unsupported protocol")
	// ErrNoSession we don't have this session
	ErrNoSession = errors.New("connection is missing a session")
)

// onRemoteClientMessage pre-process a protocol message from a remote client handling decryption and authentication
// authenticated messages are forwarded to corresponding protocol handlers
// Main incoming network messages handler
// c: connection we got this message on
// msg: binary protobufs encoded data
func (s *swarm) onRemoteClientMessage(msg net.IncomingMessageEvent) error {

	if msg.Message == nil || msg.Conn == nil {
		return ErrBadFormat1
	}

	// protocol messages are encrypted in payload
	// Locate the session
	session := msg.Conn.Session()

	if session == nil {
		return ErrNoSession
	}

	decPayload, err := session.OpenMessage(msg.Message)
	if err != nil {
		return ErrFailDecrypt
	}

	pm := &ProtocolMessage{}
	err = types.BytesToInterface(decPayload, pm)
	if err != nil {
		s.logger.Error("proto marshaling err=", err)
		return ErrBadFormat2
	}

	// check that the message was send within a reasonable time
	if ok := timesync.CheckMessageDrift(pm.Metadata.Timestamp); !ok {
		// TODO: consider kill connection with this node and maybe blacklist
		// TODO : Also consider moving send timestamp into metadata(encrypted).
		return ErrOutOfSync
	}

	data, err := ExtractData(pm.Payload)

	if err != nil {
		return err
	}

	// Add metadata collected from p2p message (todo: maybe pass sender and protocol inside metadata)
	p2pmeta := service.P2PMetadata{msg.Conn.RemoteAddr()}

	// TODO: get rid of mutexes. (Blocker: registering protocols after `Start`. currently only known place is Test_Gossiping
	s.protocolHandlerMutex.RLock()
	_, ok := s.gossipProtocolHandlers[pm.Metadata.NextProtocol]
	s.protocolHandlerMutex.RUnlock()

	s.logger.Debug("Handle %v message from <<  %v", pm.Metadata.NextProtocol, msg.Conn.RemotePublicKey().String())

	if ok {
		// pass to gossip relay chan
		return s.gossip.Relay(msg.Conn.RemotePublicKey(), pm.Metadata.NextProtocol, data)
	}

	// route authenticated message to the registered protocol
	// messages handled here are always processed by direct based protocols, only the gossip protocol calls ProcessGossipProtocolMessage
	return s.ProcessDirectProtocolMessage(msg.Conn.RemotePublicKey(), pm.Metadata.NextProtocol, data, p2pmeta)
}

// ProcessDirectProtocolMessage passes an already decrypted message to a protocol.
func (s *swarm) ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, metadata service.P2PMetadata) error {
	// route authenticated message to the registered protocol
	s.protocolHandlerMutex.RLock()
	msgchan := s.directProtocolHandlers[protocol]
	s.protocolHandlerMutex.RUnlock()
	if msgchan == nil {
		return ErrNoProtocol
	}
	s.logger.Debug("Forwarding message to %v protocol", protocol)

	metrics.QueueLength.With(metrics.ProtocolLabel, protocol).Set(float64(len(msgchan)))

	msgchan <- directProtocolMessage{metadata, sender, data}

	return nil
}

// ProcessGossipProtocolMessage passes an already decrypted message to a protocol. It is expected that the protocol will send
// the message syntactic validation result on the validationCompletedChan ASAP
func (s *swarm) ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error {
	// route authenticated message to the registered protocol
	s.protocolHandlerMutex.RLock()
	msgchan := s.gossipProtocolHandlers[protocol]
	s.protocolHandlerMutex.RUnlock()
	if msgchan == nil {
		return ErrNoProtocol
	}
	s.logger.Debug("Forwarding message to %v protocol", protocol)

	metrics.QueueLength.With(metrics.ProtocolLabel, protocol).Set(float64(len(msgchan)))

	msgchan <- gossipProtocolMessage{sender, data, validationCompletedChan}

	return nil
}

// Broadcast creates a gossip message signs it and disseminate it to neighbors.
func (s *swarm) Broadcast(protocol string, payload []byte) error {
	return s.gossip.Broadcast(payload, protocol)
}

// Neighborhood : neighborhood is the peers we keep close, meaning we try to keep connections
// to them at any given time and if not possible we replace them. Protocols use the neighborhood
// to run their logic with peers.
func (s *swarm) publishNewPeer(peer p2pcrypto.PublicKey) {
	s.peerLock.RLock()
	for _, p := range s.newPeerSub {
		select {
		case p <- peer:
		default:
		}
	}
	s.peerLock.RUnlock()
}

func (s *swarm) publishDelPeer(peer p2pcrypto.PublicKey) {
	s.peerLock.RLock()
	for _, p := range s.delPeerSub {
		select {
		case p <- peer:
		default:
		}
	}
	s.peerLock.RUnlock()
}

// SubscribePeerEvents lets clients listen on events inside the swarm about peers. first chan is new peers, second is deleted peers.
func (s *swarm) SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey) {
	in := make(chan p2pcrypto.PublicKey, 30) // todo : the size should be determined after #269
	del := make(chan p2pcrypto.PublicKey, 30)
	s.peerLock.Lock()
	s.newPeerSub = append(s.newPeerSub, in)
	s.delPeerSub = append(s.delPeerSub, del)
	s.peerLock.Unlock()
	// todo : send the existing peers right away ?
	return in, del
}

// NoResultsInterval is the timeout we wait between requesting more peers repeatedly
const NoResultsInterval = 1 * time.Second

// startNeighborhood a loop that manages the peers we are connected to all the time
// It connects to config.RandomConnections and after that maintains this number
// of connections, if a connection is closed we notify the loop that we need more peers now.
func (s *swarm) startNeighborhood() error {
	//TODO: Save and load persistent peers ?
	s.logger.Info("Neighborhood service started")

	// initial request for peers
	go s.peersLoop()
	s.morePeersReq <- struct{}{}

	return nil
}

func (s *swarm) peersLoop() {
loop:
	for {
		select {
		case <-s.morePeersReq:
			s.logger.Debug("loop: got morePeersReq")
			s.askForMorePeers()
		//todo: try getting the connections (heartbeat)
		case <-s.shutdown:
			break loop // maybe error ?
		}
	}
}

func (s *swarm) askForMorePeers() {
	s.outpeersMutex.RLock()
	numpeers := len(s.outpeers)
	s.outpeersMutex.RUnlock()
	req := s.config.SwarmConfig.RandomConnections - numpeers
	if req <= 0 {
		return
	}

	s.getMorePeers(req)

	s.outpeersMutex.RLock()
	numpeers = len(s.outpeers)
	s.outpeersMutex.RUnlock()
	// todo: better way then going in this every time ?
	if numpeers >= s.config.SwarmConfig.RandomConnections {
		s.initOnce.Do(func() {
			s.logger.Info("gossip; connected to initial required neighbors - %v", len(s.outpeers))
			close(s.initial)
			s.outpeersMutex.RLock()
			var strs []string
			for pk := range s.outpeers {
				strs = append(strs, pk.String())
			}
			s.logger.Debug("neighbors list: [%v]", strings.Join(strs, ","))
			s.outpeersMutex.RUnlock()
		})
		return
	}
	// if we could'nt get any maybe were initializing
	// wait a little bit before trying again
	tmr := time.NewTimer(NoResultsInterval)
	defer tmr.Stop()
	select {
	case <-s.shutdown:
		return
	case <-tmr.C:
		s.morePeersReq <- struct{}{}
	}
}

// getMorePeers tries to fill the `peers` slice with dialed outbound peers that we selected from the discovery.
func (s *swarm) getMorePeers(numpeers int) int {

	if numpeers == 0 {
		return 0
	}

	// discovery should provide us with random peers to connect to
	nds := s.discover.SelectPeers(s.ctx, numpeers)
	ndsLen := len(nds)
	if ndsLen == 0 {
		s.logger.Debug("Peer sampler returned nothing.")
		// this gets busy at start so we spare a second
		return 0 // zero samples here so no reason to proceed
	}

	type cnErr struct {
		n   *node.NodeInfo
		err error
	}

	res := make(chan cnErr, numpeers)

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < ndsLen; i++ {
		go func(nd *node.NodeInfo, reportChan chan cnErr) {
			if nd.PublicKey() == s.lNode.PublicKey() {
				reportChan <- cnErr{nd, errors.New("connection to self")}
				return
			}
			s.discover.Attempt(nd.PublicKey())
			addr := inet.TCPAddr{inet.ParseIP(nd.IP.String()), int(nd.ProtocolPort), ""}
			_, err := s.cPool.GetConnection(&addr, nd.PublicKey())
			reportChan <- cnErr{nd, err}
		}(nds[i], res)
	}

	total, bad := 0, 0
	tm := time.NewTimer(s.connectingTimeout) // todo: configure
loop:
	for {
		select {
		// NOTE: breaks here intentionally break the select and not the for loop
		case cne := <-res:
			total++ // We count i every time to know when to close the channel

			if cne.err != nil {
				s.logger.Debug("can't establish connection with sampled peer %v, %v", cne.n.PublicKey(), cne.err)
				bad++
				break
			}

			pk := cne.n.PublicKey()

			s.inpeersMutex.Lock()
			_, ok := s.inpeers[pk]
			s.inpeersMutex.Unlock()
			if ok {
				s.logger.Debug("not allowing peers from inbound to upgrade to outbound to prevent poisoning, peer %v", cne.n.PublicKey())
				bad++
				break
			}

			s.outpeersMutex.Lock()
			if _, ok := s.outpeers[pk]; ok {
				s.outpeersMutex.Unlock()
				s.logger.Debug("selected an already outbound peer. not counting that peer.", cne.n.PublicKey())
				bad++
				break
			}
			s.outpeers[pk] = struct{}{}
			s.outpeersMutex.Unlock()

			s.discover.Good(cne.n.PublicKey())
			s.publishNewPeer(cne.n.PublicKey())
			metrics.OutboundPeers.Add(1)
			s.logger.Debug("Neighborhood: Added peer to peer list %v", cne.n.PublicKey())
		case <-tm.C:
			break loop
		case <-s.shutdown:
			break loop
		}

		if total == ndsLen {
			break loop
		}
	}

	return total - bad
}

// Disconnect removes a peer from the neighborhood. It requests more peers if our outbound peer count is less than configured
func (s *swarm) Disconnect(peer p2pcrypto.PublicKey) {
	s.inpeersMutex.Lock()
	if _, ok := s.inpeers[peer]; ok {
		delete(s.inpeers, peer)
		s.inpeersMutex.Unlock()
		s.publishDelPeer(peer)
		metrics.InboundPeers.Add(-1)
		return
	}
	s.inpeersMutex.Unlock()

	s.outpeersMutex.Lock()
	if _, ok := s.outpeers[peer]; ok {
		delete(s.outpeers, peer)
	} else {
		s.outpeersMutex.Unlock()
		return
	}
	s.outpeersMutex.Unlock()
	s.publishDelPeer(peer)
	metrics.OutboundPeers.Add(-1)

	// todo: don't remove if we know this is a valid peer for later
	//s.discovery.Remove(peer) // address doesn't matter because we only check dhtid

	s.morePeersReq <- struct{}{}
}

// AddIncomingPeer inserts a peer to the neighborhood as a remote peer.
func (s *swarm) addIncomingPeer(n p2pcrypto.PublicKey) error {
	s.inpeersMutex.RLock()
	amnt := len(s.inpeers)
	_, exist := s.inpeers[n]
	s.inpeersMutex.RUnlock()

	if amnt >= s.config.MaxInboundPeers {
		// todo: close connection with CPOOL
		return errors.New("Reached max connectionsd")
	}

	s.inpeersMutex.Lock()
	s.inpeers[n] = struct{}{}
	s.inpeersMutex.Unlock()
	if !exist {
		s.publishNewPeer(n)
		metrics.InboundPeers.Add(1)
	}
	return nil
}

func (s *swarm) hasIncomingPeer(peer p2pcrypto.PublicKey) bool {
	s.inpeersMutex.RLock()
	_, ok := s.inpeers[peer]
	s.inpeersMutex.RUnlock()
	return ok
}

func (s *swarm) hasOutgoingPeer(peer p2pcrypto.PublicKey) bool {
	s.outpeersMutex.RLock()
	_, ok := s.outpeers[peer]
	s.outpeersMutex.RUnlock()
	return ok
}
