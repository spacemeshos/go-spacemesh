package p2p

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nattraversal"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/discovery"
	"github.com/spacemeshos/go-spacemesh/p2p/gossip"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/timesync"

	inet "net"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectingTimeout is the timeout we wait when trying to connect a neighborhood
const ConnectingTimeout = 20 * time.Second //todo: add to the config

// UPNPRetries is the number of times to retry obtaining a port due to a UPnP failure
const UPNPRetries = 20

type cPool interface {
	GetConnection(ctx context.Context, address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error)
	GetConnectionIfExists(pk p2pcrypto.PublicKey) (net.Connection, error)
	CloseConnection(key p2pcrypto.PublicKey)
	Shutdown()
}

// Switch is the heart of the p2p package. it runs and orchestrates all services within it. It provides the external interface
// for protocols to access peers or receive incoming messages.
type Switch struct {
	// configuration and maintenance fields
	started   uint32
	bootErr   error
	bootChan  chan struct{}
	gossipErr error
	gossipC   chan struct{}
	config    config.Config
	logger    log.Log
	// Context for cancel
	ctx        context.Context
	cancelFunc context.CancelFunc

	shutdownOnce sync.Once
	shutdown     chan struct{} // local request to kill the Switch from outside. e.g when local node is shutting down

	// p2p identity with private key for signing
	lNode node.LocalNode

	// map between protocol names to listening protocol handlers
	// NOTE: maybe let more than one handler register on a protocol ?
	directProtocolHandlers map[string]chan service.DirectMessage
	gossipProtocolHandlers map[string]chan service.GossipMessage

	// Networking
	network    *net.Net    // (tcp) networking service
	udpnetwork *net.UDPNet // (udp) networking service

	// connection cache
	cPool cPool

	// protocol used to gossip - disseminate messages.
	gossip *gossip.Protocol

	// discover new peers and bootstrap the node connectivity.
	discover discovery.PeerStore // peer addresses store

	// protocol switch that includes a udp networking service (this is basically a duplication of Switch)
	udpServer *UDPMux

	// members to manage connected peers/neighbors.
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

	// function to release upnp port when shutting down
	releaseUpnp func()
}

func (s *Switch) waitForBoot() error {
	<-s.bootChan
	return s.bootErr
}

func (s *Switch) waitForGossip() error {
	<-s.gossipC
	return s.gossipErr
}

// GossipReady is a chan which is closed when we established initial min connections with peers.
func (s *Switch) GossipReady() <-chan struct{} {
	if s.config.SwarmConfig.Gossip {
		return s.gossipC
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

// newSwarm creates a new P2P instance, configured by config, if a NodeID is given it tries to load if from `datadir`,
// otherwise it loads the first found node from `datadir` or creates a new one and store it there. It creates
// all the needed services.
func newSwarm(ctx context.Context, config config.Config, logger log.Log, datadir string) (*Switch, error) {
	var l node.LocalNode
	var err error

	// Load an existing identity from file if exists.
	if config.NodeID != "" {
		l, err = node.LoadIdentity(datadir, config.NodeID)
	} else {
		l, err = node.ReadFirstNodeData(datadir)
		if err != nil {
			logger.With().Warning("failed to load p2p identity from disk, creating a new identity",
				log.String("datadir", datadir),
				log.Err(err))
			l, err = node.NewNodeIdentity()
		}
	}

	if err != nil {
		return nil, err
	}

	logger.With().Info("local node identity", l.PublicKey())
	logger = logger.WithFields(log.String("p2pid", l.PublicKey().String()))

	if datadir != "" {
		if err := l.PersistData(datadir); err != nil {
			logger.With().Warning("failed to persist p2p node data", log.Err(err))
		}
	}

	// Create networking
	n, err := net.NewNet(config, l, logger.WithName("tcpnet"))
	if err != nil {
		return nil, fmt.Errorf("can't create switch without a network, err: %v", err)
	}

	udpnet, err := net.NewUDPNet(config, l, logger.WithName("udpnet"))
	if err != nil {
		return nil, err
	}

	localCtx, cancel := context.WithCancel(ctx)

	s := &Switch{
		ctx:        localCtx,
		cancelFunc: cancel,

		config: config,
		logger: logger,

		lNode: l,

		bootChan: make(chan struct{}),
		gossipC:  make(chan struct{}),
		shutdown: make(chan struct{}), // non-buffered so requests to shutdown block until Switch is shut down

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

	// Create the udp version of Switch
	mux := NewUDPMux(ctx, s.lNode, s.lookupFunc, udpnet, s.config.NetworkID, s.logger)
	s.udpServer = mux

	// todo : if discovery on
	s.discover = discovery.New(ctx, l, config.SwarmConfig, s.udpServer, datadir, s.logger) // create table and discovery protocol
	cpool := connectionpool.NewConnectionPool(s.network.Dial, l.PublicKey(), logger)
	s.network.SubscribeOnNewRemoteConnections(func(nce net.NewConnectionEvent) {
		ctx := log.WithNewSessionID(ctx)
		if err := cpool.OnNewConnection(ctx, nce); err != nil {
			s.logger.WithContext(ctx).With().Warning("adding incoming connection", log.Err(err))
			// no need to continue since this means connection already exists.
			return
		}
		s.onNewConnection(nce)
	})
	s.network.SubscribeClosingConnections(cpool.OnClosedConnection)
	s.network.SubscribeClosingConnections(s.onClosedConnection)
	s.cPool = cpool
	s.gossip = gossip.NewProtocol(config.SwarmConfig, s, peers.NewPeers(s, s.logger), s.LocalNode().PublicKey(), s.logger)
	s.logger.With().Debug("created new swarm", l.PublicKey())
	return s, nil
}

func (s *Switch) lookupFunc(target p2pcrypto.PublicKey) (*node.Info, error) {
	return s.discover.Lookup(target)
}

func (s *Switch) onNewConnection(nce net.NewConnectionEvent) {
	// todo: consider doing cpool actions from here instead of registering cpool as well.
	if err := s.addIncomingPeer(nce.Node.PublicKey()); err != nil {
		s.logger.With().Warning("error adding new connection",
			log.FieldNamed("peer_id", nce.Node.PublicKey()),
			log.Err(err))
		// todo: send rejection reason
		s.cPool.CloseConnection(nce.Node.PublicKey())
	}
}

func (s *Switch) onClosedConnection(ctx context.Context, cwe net.ConnectionWithErr) {
	// we don't want to block, we know this node's connection was closed.
	// todo: pass on closing reason, if we closed the connection.
	// 	mark address book or ban if malicious activity recognised
	s.Disconnect(cwe.Conn.RemotePublicKey())
}

// Start starts the p2p service. if configured, bootstrap is started in the background.
// returns error if the Switch is already running or there was an error starting one of the needed services.
func (s *Switch) Start(ctx context.Context) error {
	if atomic.LoadUint32(&s.started) == 1 {
		return errors.New("switch already running")
	}

	var err error

	atomic.StoreUint32(&s.started, 1)
	s.logger.Debug("starting p2p layer")

	tcpListener, udpListener, err := s.getListeners(getTCPListener, getUDPListener, discoverUPnPGateway)
	if err != nil {
		return fmt.Errorf("error getting port: %v", err)
	}

	s.network.Start(log.WithNewSessionID(ctx), tcpListener)
	s.udpnetwork.Start(log.WithNewSessionID(ctx), udpListener)

	tcpAddress := s.network.LocalAddr().(*inet.TCPAddr)
	udpAddress := s.udpnetwork.LocalAddr().(*inet.UDPAddr)

	s.discover.SetLocalAddresses(tcpAddress.Port, udpAddress.Port) // todo: pass net.Addr and convert in discovery

	if err := s.udpServer.Start(); err != nil {
		return err
	}

	s.logger.Debug("starting to listen for network messages")
	s.listenToNetworkMessages(ctx) // fires up a goroutine for each queue of messages
	s.logger.Debug("starting the udp server")

	// TODO : insert new addresses to discovery

	if s.config.SwarmConfig.Bootstrap {
		go func() {
			b := time.Now()
			if err := s.discover.Bootstrap(s.ctx); err != nil {
				s.bootErr = err
				close(s.bootChan)
				s.Shutdown()
				return
			}
			close(s.bootChan)
			size := s.discover.Size()
			s.logger.Event().Info("discovery_bootstrap",
				log.Bool("success", size >= s.config.SwarmConfig.RandomConnections && s.bootErr == nil),
				log.Int("size", size),
				log.Duration("time_elapsed", time.Since(b)))

		}()
	}

	// todo: maybe reset peers that connected us while bootstrapping
	// wait for neighborhood
	if s.config.SwarmConfig.Gossip {
		// start gossip before starting to collect peers
		s.gossip.Start(log.WithNewSessionID(ctx))
		go func() {
			if s.config.SwarmConfig.Bootstrap {
				if s.waitForBoot() != nil {
					return
				}
			}
			//todo:maybe start listening only after we got enough outbound neighbors?
			s.gossipErr = s.startNeighborhood(ctx) // non blocking

			if s.gossipErr != nil {
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

// LocalNode is the local p2p identity.
func (s *Switch) LocalNode() node.LocalNode {
	return s.lNode
}

// SendWrappedMessage sends a wrapped message in order to differentiate between request response and sub protocol messages.
// It is used by `MessageServer`.
func (s *Switch) SendWrappedMessage(ctx context.Context, nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error {
	return s.sendMessageImpl(ctx, nodeID, protocol, payload)
}

// SendMessage sends a p2p message to a peer using its public key. The provided public key must belong
// to one of our connected neighbors, otherwise an error will be returned.
func (s *Switch) SendMessage(ctx context.Context, peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	return s.sendMessageImpl(ctx, peerPubkey, protocol, service.DataBytes{Payload: payload})
}

// sendMessageImpl Sends a message to a remote node
// receives `service.Data` which is either bytes or a DataMsgWrapper.
func (s *Switch) sendMessageImpl(ctx context.Context, peerPubKey p2pcrypto.PublicKey, protocol string, payload service.Data) error {
	var err error
	var conn net.Connection

	if s.discover.IsLocalAddress(&node.Info{ID: peerPubKey.Array()}) {
		return errors.New("can't send message to self")
		//TODO: if this is our neighbor it should be removed right now.
	}

	conn, err = s.cPool.GetConnectionIfExists(peerPubKey)
	if err != nil {
		return fmt.Errorf("peer not a neighbor or connection lost: %v", err)
	}

	logger := s.logger.WithContext(ctx).WithFields(
		log.FieldNamed("from_id", s.LocalNode().PublicKey()),
		log.FieldNamed("peer_id", conn.RemotePublicKey()))

	session := conn.Session()
	if session == nil {
		logger.Warning("failed to send message to peer, no valid session")
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

	// Add context on the recipient, since it's lost in the sealed message
	ctx = context.WithValue(ctx, log.PeerIDKey, conn.RemotePublicKey().String())
	if err := conn.Send(ctx, final); err != nil {
		logger.With().Error("error sending direct message", log.Err(err))
		return err
	}

	logger.Debug("direct message sent successfully")
	return nil
}

// RegisterDirectProtocol registers an handler for a direct messaging based protocol.
func (s *Switch) RegisterDirectProtocol(protocol string) chan service.DirectMessage { // TODO: not used - remove
	if s.started == 1 {
		log.Panic("attempt to register direct protocol after p2p has started")
	}
	mchan := make(chan service.DirectMessage, s.config.BufferSize)
	s.directProtocolHandlers[protocol] = mchan
	return mchan
}

// RegisterGossipProtocol registers an handler for a gossip based protocol. priority must be provided.
func (s *Switch) RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage {
	if s.started == 1 {
		log.Panic("attempt to register gossip protocol after p2p has started")
	}
	mchan := make(chan service.GossipMessage, s.config.BufferSize)
	s.gossip.SetPriority(protocol, prio)
	s.gossipProtocolHandlers[protocol] = mchan
	return mchan
}

// Shutdown sends a shutdown signal to all running services of Switch and then runs an internal shutdown to cleanup.
func (s *Switch) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.cancelFunc()
		close(s.shutdown)
		s.gossip.Close()
		s.discover.Shutdown()
		s.cPool.Shutdown()
		s.network.Shutdown()
		// udpServer (udpMux) shuts down the udpnet as well
		s.udpServer.Shutdown()

		for i := range s.directProtocolHandlers {
			delete(s.directProtocolHandlers, i)
			//close(prt) //todo: signal protocols to shutdown with closing chan. (this makes us send on closed chan. )
		}
		for i := range s.gossipProtocolHandlers {
			delete(s.gossipProtocolHandlers, i)
			//close(prt) //todo: signal protocols to shutdown with closing chan. (this makes us send on closed chan. )
		}

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
		if s.releaseUpnp != nil {
			s.releaseUpnp()
		}
	})
}

// process an incoming message
func (s *Switch) processMessage(ctx context.Context, ime net.IncomingMessageEvent) {
	// Extract request context and add to log
	if ime.RequestID != "" {
		ctx = log.WithRequestID(ctx, ime.RequestID)
	} else {
		ctx = log.WithNewRequestID(ctx)
		s.logger.WithContext(ctx).Warning("got incoming message event with no requestID, setting new id")
	}

	if s.config.MsgSizeLimit != config.UnlimitedMsgSize && len(ime.Message) > s.config.MsgSizeLimit {
		s.logger.WithContext(ctx).With().Error("message is too big to process",
			log.Int("limit", s.config.MsgSizeLimit),
			log.Int("actual", len(ime.Message)))
		return
	}

	if err := s.onRemoteClientMessage(ctx, ime); err != nil {
		// TODO: differentiate action on errors
		s.logger.WithContext(ctx).With().Error("err reading incoming message, closing connection",
			log.FieldNamed("sender_id", ime.Conn.RemotePublicKey()),
			log.Err(err))
		if err := ime.Conn.Close(); err == nil {
			s.cPool.CloseConnection(ime.Conn.RemotePublicKey())
			s.Disconnect(ime.Conn.RemotePublicKey())
		}
	}
}

// RegisterDirectProtocolWithChannel registers a direct protocol with a given channel. NOTE: eventually should replace RegisterDirectProtocol
func (s *Switch) RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage {
	if s.started == 1 {
		log.Panic("attempting to register direct protocol with channel after p2p has started")
	}
	s.directProtocolHandlers[protocol] = ingressChannel
	return ingressChannel
}

// listenToNetworkMessages is listening on new messages from the opened connections and processes them.
func (s *Switch) listenToNetworkMessages(ctx context.Context) {
	// We listen to each of the messages queues we get from `net`
	// It's net's responsibility to distribute the messages to the queues
	// in a way that they're processing order will work
	// Switch process all the queues concurrently but synchronously for each queue
	netqueues := s.network.IncomingMessages()
	for nq := range netqueues { // run a separate worker for each queue.
		ctx := log.WithNewSessionID(ctx)
		go func(c chan net.IncomingMessageEvent) {
			for {
				select {
				case msg := <-c:
					// requestID will be restored from the saved message, no need to generate one here
					s.processMessage(ctx, msg)
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
	ErrBadFormat1 = errors.New("bad msg format, couldn't deserialize 1")
	// ErrBadFormat2 could'nt deserialize the protocol message payload
	ErrBadFormat2 = errors.New("bad msg format, couldn't deserialize 2")
	// ErrOutOfSync is returned when message timestamp was out of sync
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
// msg : an incoming message event that includes the raw message and the sending connection.
func (s *Switch) onRemoteClientMessage(ctx context.Context, msg net.IncomingMessageEvent) error {
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
	if err = types.BytesToInterface(decPayload, pm); err != nil {
		s.logger.With().Error("error deserializing message", log.Err(err))
		return ErrBadFormat2
	}

	// check that the message was sent within a reasonable time
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
	p2pmeta := service.P2PMetadata{FromAddress: msg.Conn.RemoteAddr()}

	_, ok := s.gossipProtocolHandlers[pm.Metadata.NextProtocol]

	s.logger.WithContext(ctx).With().Debug("handle incoming message",
		log.String("protocol", pm.Metadata.NextProtocol),
		log.FieldNamed("sender_id", msg.Conn.RemotePublicKey()),
		log.Bool("is_gossip", ok))

	if ok {
		// if this message is tagged with a gossip protocol, relay it.
		return s.gossip.Relay(ctx, msg.Conn.RemotePublicKey(), pm.Metadata.NextProtocol, data)
	}

	// route authenticated message to the registered protocol
	// messages handled here are always processed by direct based protocols, only the gossip protocol calls ProcessGossipProtocolMessage
	return s.ProcessDirectProtocolMessage(ctx, msg.Conn.RemotePublicKey(), pm.Metadata.NextProtocol, data, p2pmeta)
}

// ProcessDirectProtocolMessage passes an already decrypted message to a protocol. if protocol does not exist, return and error.
func (s *Switch) ProcessDirectProtocolMessage(ctx context.Context, sender p2pcrypto.PublicKey, protocol string, data service.Data, metadata service.P2PMetadata) error {
	msgchan := s.directProtocolHandlers[protocol]
	if msgchan == nil {
		return ErrNoProtocol
	}
	s.logger.WithContext(ctx).With().Debug("forwarding direct message to protocol",
		log.Int("queue_length", len(msgchan)),
		log.String("protocol", protocol))

	metrics.QueueLength.With(metrics.ProtocolLabel, protocol).Set(float64(len(msgchan)))

	// TODO: check queue length
	msgchan <- directProtocolMessage{metadata, sender, data}
	return nil
}

// ProcessGossipProtocolMessage passes an already decrypted message to a protocol. It is expected that the protocol will send
// the message syntactic validation result on the validationCompletedChan ASAP
func (s *Switch) ProcessGossipProtocolMessage(ctx context.Context, sender p2pcrypto.PublicKey, ownMessage bool, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error {
	h := types.CalcMessageHash12(data.Bytes(), protocol)

	// route authenticated message to the registered protocol
	msgchan := s.gossipProtocolHandlers[protocol]
	if msgchan == nil {
		return ErrNoProtocol
	}
	s.logger.WithContext(ctx).With().Debug("forwarding gossip message to protocol",
		log.String("protocol", protocol),
		log.Int("queue_length", len(msgchan)),
		h)

	metrics.QueueLength.With(metrics.ProtocolLabel, protocol).Set(float64(len(msgchan)))

	// TODO: check queue length
	gpm := gossipProtocolMessage{sender: sender, ownMessage: ownMessage, data: data, validationChan: validationCompletedChan}
	if requestID, ok := log.ExtractRequestID(ctx); ok {
		gpm.requestID = requestID
	} else {
		s.logger.WithContext(ctx).With().Warning("no requestId set in context processing gossip message",
			log.String("protocol", protocol),
			h)
	}
	msgchan <- gpm
	return nil
}

// Broadcast creates a gossip message signs it and disseminate it to neighbors.
// this message must be validated by our own node first just as any other message.
func (s *Switch) Broadcast(ctx context.Context, protocol string, payload []byte) error {
	// context should already contain a requestID for an incoming message
	if _, ok := log.ExtractRequestID(ctx); !ok {
		ctx = log.WithNewRequestID(ctx)
		s.logger.WithContext(ctx).Info("new broadcast message with no requestId, generated one")
	}
	return s.gossip.Broadcast(ctx, payload, protocol)
}

// Neighborhood : a small circle of peers we try to keep connections to. if a connection
// is closed we grab a new peer and connect it. we try to achieve a steady number of
// outgoing connections. protocols can use these peers to send direct messages or gossip.

// tells protocols  we connected to a new peer.
func (s *Switch) publishNewPeer(peer p2pcrypto.PublicKey) {
	s.peerLock.RLock()
	for _, p := range s.newPeerSub {
		select {
		case p <- peer:
		default:
		}
	}
	s.peerLock.RUnlock()
}

// tells protocols  we disconnected a peer.
func (s *Switch) publishDelPeer(peer p2pcrypto.PublicKey) {
	s.peerLock.RLock()
	for _, p := range s.delPeerSub {
		select {
		case p <- peer:
		default:
		}
	}
	s.peerLock.RUnlock()
}

// SubscribePeerEvents lets clients listen on events inside the Switch about peers. first chan is new peers, second is deleted peers.
func (s *Switch) SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey) {
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

// startNeighborhood starts the peersLoop and send a request to start connecting peers.
func (s *Switch) startNeighborhood(ctx context.Context) error {
	//TODO: Save and load persistent peers ?
	s.logger.WithContext(ctx).Info("neighborhood service started")

	// initial request for peers
	go s.peersLoop(ctx)
	s.morePeersReq <- struct{}{}

	return nil
}

// peersLoop executes one routine at a time to connect new peers.
func (s *Switch) peersLoop(ctx context.Context) {
loop:
	for {
		select {
		case <-s.morePeersReq:
			s.logger.WithContext(ctx).Debug("loop: got morePeersReq")
			s.askForMorePeers(ctx)
		//todo: try getting the connections (heartbeat)
		case <-s.shutdown:
			break loop // maybe error ?
		}
	}
}

func (s *Switch) closeInitial() {
	select {
	case <-s.initial:
		// Nothing to do if channel is closed.
	default:
		// Close channel if it is not closed.
		close(s.initial)
	}
}

// askForMorePeers checks the number of peers required and tries to match this number. if there are enough peers it returns.
// if it failed it issues a one second timeout and then sends a request to try again.
func (s *Switch) askForMorePeers(ctx context.Context) {
	// check how much peers needed
	s.outpeersMutex.RLock()
	numpeers := len(s.outpeers)
	s.outpeersMutex.RUnlock()
	req := s.config.SwarmConfig.RandomConnections - numpeers
	if req <= 0 {
		// If 0 connections are required, the condition above is always true,
		// so gossip needs to be considered ready in this case.
		if s.config.SwarmConfig.RandomConnections == 0 {
			select {
			case <-s.initial:
				// Nothing to do if channel is closed.
				break
			default:
				// Close channel if it is not closed.
				close(s.initial)
			}
		}
		return
	}

	// try to connect eq peers
	s.getMorePeers(ctx, req)

	// check number of peers after
	s.outpeersMutex.RLock()
	numpeers = len(s.outpeers)
	s.outpeersMutex.RUnlock()
	// announce if initial number of peers achieved
	// todo: better way then going in this every time ?
	if numpeers >= s.config.SwarmConfig.RandomConnections {
		s.initOnce.Do(func() {
			s.logger.WithContext(ctx).With().Info("gossip connected to initial required neighbors",
				log.Int("n", len(s.outpeers)))
			s.closeInitial()
			s.outpeersMutex.RLock()
			var strs []string
			for pk := range s.outpeers {
				strs = append(strs, pk.String())
			}
			s.logger.WithContext(ctx).With().Debug("neighbors list",
				log.String("neighbors", strings.Join(strs, ",")))
			s.outpeersMutex.RUnlock()
		})
		return
	}

	s.logger.Warning("needs more %d peers", s.config.SwarmConfig.RandomConnections-numpeers)

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

// getMorePeers tries to fill the `outpeers` slice with dialed outbound peers that we selected from the discovery.
func (s *Switch) getMorePeers(ctx context.Context, numpeers int) int {
	if numpeers == 0 {
		return 0
	}

	logger := s.logger.WithContext(ctx)

	// discovery should provide us with random peers to connect to
	nds := s.discover.SelectPeers(s.ctx, numpeers)
	ndsLen := len(nds)
	if ndsLen == 0 {
		logger.Debug("peer sampler returned nothing")
		// this gets busy at start so we spare a second
		return 0 // zero samples here so no reason to proceed
	}

	type cnErr struct {
		n   *node.Info
		err error
	}

	res := make(chan cnErr, numpeers)

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < ndsLen; i++ {
		go func(nd *node.Info, reportChan chan cnErr) {
			if nd.PublicKey() == s.lNode.PublicKey() {
				reportChan <- cnErr{nd, errors.New("connection to self")}
				return
			}
			s.discover.Attempt(nd.PublicKey())
			addr := inet.TCPAddr{IP: inet.ParseIP(nd.IP.String()), Port: int(nd.ProtocolPort)}
			_, err := s.cPool.GetConnection(ctx, &addr, nd.PublicKey())
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
				logger.With().Debug("can't establish connection with sampled peer",
					log.FieldNamed("peer_id", cne.n.PublicKey()),
					log.Err(cne.err))
				bad++
				break
			}

			pk := cne.n.PublicKey()

			s.inpeersMutex.Lock()
			_, ok := s.inpeers[pk]
			s.inpeersMutex.Unlock()
			if ok {
				logger.With().Debug("not allowing peers from inbound to upgrade to outbound to prevent poisoning",
					log.FieldNamed("peer_id", cne.n.PublicKey()))
				bad++
				break
			}

			s.outpeersMutex.Lock()
			if _, ok := s.outpeers[pk]; ok {
				s.outpeersMutex.Unlock()
				logger.With().Debug("selected an already outbound peer, not counting peer",
					log.FieldNamed("peer_id", cne.n.PublicKey()))
				bad++
				break
			}
			s.outpeers[pk] = struct{}{}
			s.outpeersMutex.Unlock()

			s.discover.Good(cne.n.PublicKey())
			s.publishNewPeer(cne.n.PublicKey())
			metrics.OutboundPeers.Add(1)
			logger.With().Debug("added peer to peer list",
				log.FieldNamed("peer_id", cne.n.PublicKey()))
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
func (s *Switch) Disconnect(peer p2pcrypto.PublicKey) {
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

// addIncomingPeer inserts a peer to the neighborhood as a remote peer.
// returns an error if we reached our maximum number of peers.
func (s *Switch) addIncomingPeer(n p2pcrypto.PublicKey) error {
	s.inpeersMutex.RLock()
	amnt := len(s.inpeers)
	_, exist := s.inpeers[n]
	s.inpeersMutex.RUnlock()

	if amnt >= s.config.MaxInboundPeers {
		// todo: close connection with CPOOL
		return errors.New("reached max connections")
	}

	s.inpeersMutex.Lock()
	s.inpeers[n] = struct{}{}
	s.inpeersMutex.Unlock()
	if !exist {
		s.publishNewPeer(n)
		s.discover.Attempt(n) // or good?
		metrics.InboundPeers.Add(1)
	}
	return nil
}

func (s *Switch) hasIncomingPeer(peer p2pcrypto.PublicKey) bool {
	s.inpeersMutex.RLock()
	_, ok := s.inpeers[peer]
	s.inpeersMutex.RUnlock()
	return ok
}

func (s *Switch) hasOutgoingPeer(peer p2pcrypto.PublicKey) bool {
	s.outpeersMutex.RLock()
	_, ok := s.outpeers[peer]
	s.outpeersMutex.RUnlock()
	return ok
}

// network methods used to accumulate os and router ports.

func (s *Switch) getListeners(
	getTCPListener func(tcpAddr *inet.TCPAddr) (inet.Listener, error),
	getUDPListener func(udpAddr *inet.UDPAddr) (net.UDPListener, error),
	discoverUpnpGateway func() (nattraversal.UPNPGateway, error),
) (inet.Listener, net.UDPListener, error) {

	port := s.config.TCPPort
	randomPort := port == 0
	var gateway nattraversal.UPNPGateway
	if s.config.AcquirePort {
		s.logger.Info("trying to acquire ports using UPnP")
		var err error
		gateway, err = discoverUpnpGateway()
		if err != nil {
			gateway = nil
			s.logger.With().Warning("could not discover UPnP gateway", log.Err(err))
		}
	}

	upnpFails := 0
	var listeningIP = inet.ParseIP(s.config.TCPInterface)
	for {
		tcpAddr := &inet.TCPAddr{IP: listeningIP, Port: port}
		tcpListener, err := getTCPListener(tcpAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to acquire requested tcp port: %v", err)
		}

		addr := tcpListener.Addr()
		port = addr.(*inet.TCPAddr).Port
		udpAddr := &inet.UDPAddr{IP: listeningIP, Port: port}
		udpListener, err := getUDPListener(udpAddr)
		if err != nil {
			if err := tcpListener.Close(); err != nil {
				s.logger.With().Error("error closing tcp listener", log.Err(err))
			}
			if randomPort {
				port = 0
				continue
			}
			return nil, nil, fmt.Errorf("failed to acquire requested udp port: %v", err)
		}

		if gateway != nil {
			err := nattraversal.AcquirePortFromGateway(gateway, uint16(port))
			if err != nil {
				if upnpFails >= UPNPRetries {
					return tcpListener, udpListener, nil
				}
				if randomPort {
					if err := tcpListener.Close(); err != nil {
						s.logger.With().Error("error closing tcp listener", log.Err(err))
					}
					if err := udpListener.Close(); err != nil {
						s.logger.With().Error("error closing udp listener", log.Err(err))
					}
					port = 0
					upnpFails++
					continue
				}
				s.logger.With().Warning("failed to acquire requested port using UPnP", log.Err(err))
			} else {
				s.releaseUpnp = func() {
					if err := gateway.Clear(uint16(port)); err != nil {
						s.logger.With().Warning("failed to release acquired UPnP port",
							log.Int("port", port),
							log.Err(err))
					}
				}
			}
		}

		return tcpListener, udpListener, nil
	}
}

func getUDPListener(udpAddr *inet.UDPAddr) (net.UDPListener, error) {
	return inet.ListenUDP("udp", udpAddr)
}

func getTCPListener(tcpAddr *inet.TCPAddr) (inet.Listener, error) {
	return inet.Listen("tcp", tcpAddr.String())
}

func discoverUPnPGateway() (igd nattraversal.UPNPGateway, err error) {
	return nattraversal.DiscoverUPNPGateway()
}
