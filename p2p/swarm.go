package p2p

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/gossip"
	"github.com/spacemeshos/go-spacemesh/p2p/message"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	inet "net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectingTimeout is the timeout we wait when trying to connect a neighborhood
const ConnectingTimeout = 20 * time.Second //todo: add to the config

type protocolMessage struct {
	sender node.Node
	data   service.Data
}

func (pm protocolMessage) Sender() node.Node {
	return pm.sender
}

func (pm protocolMessage) Data() service.Data {
	return pm.data
}

func (pm protocolMessage) Bytes() []byte {
	return pm.data.Bytes()
}

type cPool interface {
	GetConnection(address string, pk crypto.PublicKey) (net.Connection, error)
	RemoteConnectionsChannel() chan net.NewConnectionEvent
}

type swarm struct {
	// init info props
	started   uint32
	bootErr   error
	bootChan  chan struct{}
	gossipErr error
	gossipC   chan struct{}

	config config.Config

	// Context for cancel
	ctx context.Context

	// Shutdown the loop
	shutdown chan struct{} // local request to kill the swarm from outside. e.g when local node is shutting down

	// set in construction and immutable state
	lNode *node.LocalNode

	// map between protocol names to listening protocol handlers
	// NOTE: maybe let more than one handler register on a protocol ?
	protocolHandlers     map[string]chan service.Message
	protocolHandlerMutex sync.RWMutex

	gossip  *gossip.Protocol
	network *net.Net
	cPool   cPool
	dht     dht.DHT

	//neighborhood
	initOnce sync.Once
	initial  chan struct{}

	outpeersMutex sync.RWMutex
	inpeersMutex  sync.RWMutex
	outpeers      map[string]crypto.PublicKey
	inpeers       map[string]crypto.PublicKey

	morePeersReq      chan struct{}
	connectingTimeout time.Duration

	peerLock   sync.RWMutex
	newPeerSub []chan crypto.PublicKey
	delPeerSub []chan crypto.PublicKey
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
// and not load from disk. it creates a new `net`, connection pool and dht.
func newSwarm(ctx context.Context, config config.Config, newNode bool, persist bool) (*swarm, error) {

	port := config.TCPPort
	address := inet.JoinHostPort("0.0.0.0", strconv.Itoa(port))

	var l *node.LocalNode
	var err error
	// Load an existing identity from file if exists.

	if newNode {
		l, err = node.NewNodeIdentity(config, address, persist)
	} else {
		l, err = node.NewLocalNode(config, address, persist)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create a node, err: %v", err)
	}

	n, err := net.NewNet(config, l)
	if err != nil {
		return nil, fmt.Errorf("can't create swarm without a network, err: %v", err)
	}

	s := &swarm{
		ctx:      ctx,
		config:   config,
		lNode:    l,
		bootChan: make(chan struct{}),
		gossipC:  make(chan struct{}),
		shutdown: make(chan struct{}), // non-buffered so requests to shutdown block until swarm is shut down

		initial:           make(chan struct{}),
		morePeersReq:      make(chan struct{}),
		inpeers:           make(map[string]crypto.PublicKey),
		outpeers:          make(map[string]crypto.PublicKey),
		newPeerSub:        make([]chan crypto.PublicKey, 0, 10),
		delPeerSub:        make([]chan crypto.PublicKey, 0, 10),
		connectingTimeout: ConnectingTimeout,

		protocolHandlers: make(map[string]chan service.Message),
		network:          n,
		cPool:            connectionpool.NewConnectionPool(n, l.PublicKey()),
	}

	s.dht = dht.New(l, config.SwarmConfig, s)

	s.gossip = gossip.NewProtocol(config.SwarmConfig, s, newSignerValidator(s), s.lNode.Log)

	s.lNode.Debug("Created swarm for local node %s, %s", l.Address(), l.Pretty())

	return s, nil
}

type signerValidator struct {
	pubKey   crypto.PublicKey
	signFunc func([]byte) ([]byte, error)
}

// PublicKey is the signerValidtor pair pub key
func (sv *signerValidator) PublicKey() crypto.PublicKey {
	return sv.pubKey
}

// Sign is delegating the sign function form the private key
func (sv *signerValidator) Sign(data []byte) ([]byte, error) {
	return sv.signFunc(data)
}

func newSignerValidator(s *swarm) *signerValidator {
	return &signerValidator{s.lNode.PublicKey(), s.lNode.PrivateKey().Sign}
}

func (s *swarm) Start() error {
	if atomic.LoadUint32(&s.started) == 1 {
		return errors.New("swarm already running")
	}
	atomic.StoreUint32(&s.started, 1)
	s.lNode.Debug("Starting the p2p layer")

	go s.handleNewConnectionEvents()

	s.listenToNetworkMessages() // fires up a goroutine for each queue of messages

	go s.checkTimeDrifts()

	if s.config.SwarmConfig.Bootstrap {
		go func() {
			b := time.Now()
			err := s.dht.Bootstrap(s.ctx)
			if err != nil {
				s.bootErr = err
				close(s.bootChan)
				s.Shutdown()
				return
			}
			close(s.bootChan)
			s.lNode.Info("DHT Bootstrapped with %d peers in %v", s.dht.Size(), time.Since(b))
		}()
	}

	// start gossip before starting to collect peers

	s.gossip.Start() // non-blocking

	// wait for neighborhood
	go func() {
		if s.config.SwarmConfig.Bootstrap {
			s.waitForBoot()
		}
		err := s.startNeighborhood()
		if err != nil {
			s.gossipErr = err
			close(s.gossipC)
			s.Shutdown()
			return
		}
		<-s.initial
		close(s.gossipC)
	}() // todo handle error async

	return nil
}

func (s *swarm) LocalNode() *node.LocalNode {
	return s.lNode
}

func (s *swarm) connectionPool() cPool {
	return s.cPool
}

func (s *swarm) SendWrappedMessage(nodeID string, protocol string, payload *service.DataMsgWrapper) error {
	return s.sendMessageImpl(nodeID, protocol, payload)
}

func (s *swarm) SendMessage(nodeID string, protocol string, payload []byte) error {
	return s.sendMessageImpl(nodeID, protocol, service.DataBytes{Payload: payload})
}

// SendMessage Sends a message to a remote node
// swarm will establish session if needed or use an existing session and open connection
// Designed to be used by any high level protocol
// req.reqID: globally unique id string - used for tracking messages we didn't get a response for yet
// req.msg: marshaled message data
// req.destId: receiver remote node public key/id
// Local request to send a message to a remote node
func (s *swarm) sendMessageImpl(peerPubKey string, protocol string, payload service.Data) error {
	//s.lNode.Info("Sending message to %v", peerPubKey)
	var err error
	var peer node.Node
	var conn net.Connection

	peer, err = s.dht.Lookup(peerPubKey) // blocking, might issue a network lookup that'll take time.
	if err != nil {
		return err
	}

	conn, err = s.cPool.GetConnection(peer.Address(), peer.PublicKey()) // blocking, might take some time in case there is no connection
	if err != nil {
		s.lNode.Warning("failed to send message to %v, no valid connection. err: %v", peer.String(), err)
		return err
	}

	session := conn.Session()
	if session == nil {
		s.lNode.Warning("failed to send message to %v, no valid session. err: %v", peer.String(), err)
		return err
	}

	protomessage := &pb.ProtocolMessage{
		Metadata: message.NewProtocolMessageMetadata(s.lNode.PublicKey(), protocol),
	}

	switch x := payload.(type) {
	case service.DataBytes:
		protomessage.Data = &pb.ProtocolMessage_Payload{Payload: x.Bytes()}
	case *service.DataMsgWrapper:
		protomessage.Data = &pb.ProtocolMessage_Msg{Msg: &pb.MessageWrapper{Type: x.MsgType, Req: x.Req, ReqID: x.ReqID, Payload: x.Payload}}
	case nil:
		// The field is not set.
	default:
		return fmt.Errorf("protocolMsg has unexpected type %T", x)
	}

	err = message.SignMessage(s.lNode.PrivateKey(), protomessage)
	if err != nil {
		return err
	}

	data, err := proto.Marshal(protomessage)
	if err != nil {
		return fmt.Errorf("failed to encode signed message err: %v", err)
	}

	session.EncryptGuard().Lock()

	// messages must be sent in the same order as the order that the messages were encrypted because the iv used to encrypt
	// (and therefore decrypt) is the last encrypted block of the previous message that were encrypted
	final, err := message.PrepareMessage(session, data)

	if err != nil {
		session.EncryptGuard().Unlock()
		// since it is possible that the encryption succeeded and the iv was modified for the next message, we must close the connection otherwise
		// the missing message will prevent the receiver from decrypting any future message
		s.lNode.Logger.Error("prepare message failed, closing the connection")
		conn.Close()
		e := fmt.Errorf("aborting send - failed to encrypt protocolMsg: %v", err)
		return e
	}

	err = conn.Send(final)
	session.EncryptGuard().Unlock()

	s.lNode.Debug("Message sent succesfully")

	return err
}

// RegisterProtocol registers an handler for `protocol`
func (s *swarm) RegisterProtocol(protocol string) chan service.Message {
	mchan := make(chan service.Message, 100)
	s.protocolHandlerMutex.Lock()
	s.protocolHandlers[protocol] = mchan
	s.protocolHandlerMutex.Unlock()
	return mchan
}

// Shutdown sends a shutdown signal to all running services of swarm and then runs an internal shutdown to cleanup.
func (s *swarm) Shutdown() {
	close(s.shutdown)
	<-s.shutdown // Block until really closes.
	s.shutdownInternal()
}

// shutdown gracefully shuts down swarm services.
func (s *swarm) shutdownInternal() {
	//TODO : Gracefully shutdown swarm => finish incmoing / outgoing msgs
	s.network.Shutdown()
}

// process an incoming message
func (s *swarm) processMessage(ime net.IncomingMessageEvent) {
	select {
	case <-s.shutdown:
		break
	default:
		err := s.onRemoteClientMessage(ime)
		if err != nil {
			s.lNode.Errorf("Err reading message from %v, closing connection err=%v", ime.Conn.RemotePublicKey(), err)
			ime.Conn.Close()
			// TODO: differentiate action on errors
		}
	}
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

func (s *swarm) handleNewConnectionEvents() {
	newConnEvents := s.cPool.RemoteConnectionsChannel()
	closing := s.network.SubscribeClosingConnections()
Loop:
	for {
		select {
		case con := <-closing:
			go s.retryOrReplace(con.RemotePublicKey()) //todo notify dht?
		case nce := <-newConnEvents:
			go func(nce net.NewConnectionEvent) { s.dht.Update(nce.Node); s.addIncomingPeer(nce.Node.PublicKey()) }(nce)
		case <-s.shutdown:
			break Loop
		}
	}
}

func (s *swarm) retryOrReplace(key crypto.PublicKey) {
	getpeer := s.dht.InternalLookup(node.NewDhtID(key.Bytes()))

	if getpeer == nil {
		s.Disconnect(key) // if we didn't find then we can't try replaceing
		return
	}
	peer := getpeer[0]

	_, err := s.cPool.GetConnection(peer.Address(), peer.PublicKey())
	if err != nil { // we could'nt connect :/
		s.Disconnect(key)
	}
}

// periodically checks that our clock is sync
func (s *swarm) checkTimeDrifts() {
	checkTimeSync := time.NewTicker(config.TimeConfigValues.RefreshNtpInterval)
Loop:
	for {
		select {
		case <-s.shutdown:
			break Loop

		case <-checkTimeSync.C:
			_, err := timesync.CheckSystemClockDrift()
			if err != nil {
				checkTimeSync.Stop()
				s.lNode.Error("System time could'nt synchronize %s", err)
				s.Shutdown()
			}
		}
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
	// ErrAuthAuthor message sign is wrong
	ErrAuthAuthor = errors.New("failed to verify author")
	// ErrNoProtocol we don't have the protocol message
	ErrNoProtocol = errors.New("received msg to an unsupported protocol")
	// ErrNoSession we don't have this session
	ErrNoSession = errors.New("connection is missing a session")
	// ErrNotFromPeer - we got message singed with a different publickkey and its not gossip
	ErrNotFromPeer = errors.New("this message was signed with the wrong public key")
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

	s.lNode.Debug(fmt.Sprintf("Handle message from <<  %v", msg.Conn.RemotePublicKey().Pretty()))

	// protocol messages are encrypted in payload
	// Locate the session
	session := msg.Conn.Session()

	if session == nil {
		return ErrNoSession
	}

	decPayload, err := session.Decrypt(msg.Message)
	if err != nil {
		return ErrFailDecrypt
	}

	pm := &pb.ProtocolMessage{}
	err = proto.Unmarshal(decPayload, pm)
	if err != nil {
		s.lNode.Errorf("proto marshinling err=", err)
		return ErrBadFormat2
	}

	// authenticate valid pubkey, same as session remote pubkey and validate sign.
	authPub, err := crypto.NewPublicKey(pm.Metadata.AuthPubKey)
	if err != nil {
		return ErrAuthAuthor
	}

	if !bytes.Equal(authPub.Bytes(), msg.Conn.RemotePublicKey().Bytes()) {
		return ErrAuthAuthor
	}

	err = message.AuthAuthor(pm)
	if err != nil {
		return ErrAuthAuthor
	}

	// check that the message was send within a reasonable time
	if ok := timesync.CheckMessageDrift(pm.Metadata.Timestamp); !ok {
		// TODO: consider kill connection with this node and maybe blacklist
		// TODO : Also consider moving send timestamp into metadata(encrypted).
		return ErrOutOfSync
	}

	s.lNode.Debug("Authorized %v protocol message ", pm.Metadata.NextProtocol)

	remoteNode := node.New(msg.Conn.RemotePublicKey(), "") // if we got so far, we already have the node in our rt, hence address won't be used
	// update the routing table - we just heard from this authenticated node
	s.dht.Update(remoteNode)

	// route authenticated message to the registered protocol

	var data service.Data

	if payload := pm.GetPayload(); payload != nil {
		data = service.DataBytes{Payload: payload}
	} else if wrap := pm.GetMsg(); wrap != nil {
		data = &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.Type, ReqID: wrap.ReqID, Payload: wrap.Payload}
	}

	return s.ProcessProtocolMessage(remoteNode, pm.Metadata.NextProtocol, data)
}

// ProcessProtocolMessage passes an already decrypted message to a protocol.
func (s *swarm) ProcessProtocolMessage(sender node.Node, protocol string, data service.Data) error {
	// route authenticated message to the reigstered protocol
	s.protocolHandlerMutex.RLock()
	msgchan := s.protocolHandlers[protocol]
	s.protocolHandlerMutex.RUnlock()
	if msgchan == nil {
		return ErrNoProtocol
	}
	s.lNode.Debug("Forwarding message to %v protocol", protocol)

	msgchan <- protocolMessage{sender, data}

	return nil
}

// Broadcast creates a gossip message signs it and disseminate it to neighbors.
func (s *swarm) Broadcast(protocol string, payload []byte) error {
	return s.gossip.Broadcast(payload, protocol)
}

// Neighborhood : neighborhood is the peers we keep close , meaning we try to keep connections
// to them at any given time and if not possible we replace them. protocols use the neighborhood
// to run their logic with peers.

func (s *swarm) publishNewPeer(peer crypto.PublicKey) {
	s.peerLock.RLock()
	for _, p := range s.newPeerSub {
		select {
		case p <- peer:
		default:
		}
	}
	s.peerLock.RUnlock()
}

func (s *swarm) publishDelPeer(peer crypto.PublicKey) {
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
func (s *swarm) SubscribePeerEvents() (chan crypto.PublicKey, chan crypto.PublicKey) {
	in := make(chan crypto.PublicKey, s.config.SwarmConfig.RandomConnections) // todo. what size this should be ? maybe let client pass channels.
	del := make(chan crypto.PublicKey, s.config.SwarmConfig.RandomConnections)
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
	s.lNode.Info("Neighborhood service started")

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
			s.lNode.Debug("loop: got morePeersReq")
			go s.askForMorePeers()
		//todo: try getting the connections (hearbeat)
		case <-s.shutdown:
			break loop // maybe error ?
		}
	}
}

func (s *swarm) askForMorePeers() {
	numpeers := len(s.outpeers)
	req := s.config.SwarmConfig.RandomConnections - numpeers
	if req <= 0 {
		return
	}

	s.getMorePeers(req)

	// todo: better way then going in this everytime ?
	if len(s.outpeers) >= s.config.SwarmConfig.RandomConnections {
		s.initOnce.Do(func() {
			s.lNode.Info("gossip; connected to initial required neighbors - %v", len(s.outpeers))
			close(s.initial)
			s.outpeersMutex.RLock()
			s.lNode.Debug(spew.Sdump(s.outpeers))
			s.outpeersMutex.RUnlock()
		})
		return
	}
	// if we could'nt get any maybe were initializing
	// wait a little bit before trying again
	time.Sleep(NoResultsInterval)
	s.morePeersReq <- struct{}{}
}

// getMorePeers tries to fill the `peers` slice with dialed outbound peers that we selected from the dht.
func (s *swarm) getMorePeers(numpeers int) int {

	if numpeers == 0 {
		return 0
	}

	// dht should provide us with random peers to connect to
	nds := s.dht.SelectPeers(numpeers)
	ndsLen := len(nds)
	if ndsLen == 0 {
		s.lNode.Debug("Peer sampler returned nothing.")
		// this gets busy at start so we spare a second
		return 0 // zero samples here so no reason to proceed
	}

	type cnErr struct {
		n   node.Node
		err error
	}

	res := make(chan cnErr, numpeers)

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < ndsLen; i++ {
		go func(nd node.Node, reportChan chan cnErr) {
			_, err := s.cPool.GetConnection(nd.Address(), nd.PublicKey())
			reportChan <- cnErr{nd, err}
		}(nds[i], res)
	}

	total, bad := 0, 0
	tm := time.NewTimer(s.connectingTimeout) // todo: configure
loop:
	for {
		select {
		case cne := <-res:
			total++ // We count i everytime to know when to close the channel

			if cne.err != nil {
				s.lNode.Debug("can't establish connection with sampled peer %v, %v", cne.n.String(), cne.err)
				bad++
				if total == ndsLen {
					break loop
				}
				continue // this peer didn't work, todo: tell dht
			}

			s.inpeersMutex.Lock()
			_, ok := s.inpeers[cne.n.PublicKey().String()]
			s.inpeersMutex.Unlock()
			if ok {
				s.lNode.Debug("not allowing peers from inbound to upgrade to outbound to prevent poisoning, peer %v", cne.n.String())
				bad++
				if total == ndsLen {
					break loop
				}
				continue

			}

			s.outpeersMutex.Lock()
			s.outpeers[cne.n.PublicKey().String()] = cne.n.PublicKey()
			s.outpeersMutex.Unlock()

			s.publishNewPeer(cne.n.PublicKey())
			s.lNode.Debug("Neighborhood: Added peer to peer list %v", cne.n.Pretty())

			if total == ndsLen {
				break loop
			}
		case <-tm.C:
			break loop
		case <-s.shutdown:
			break loop
		}
	}

	return total - bad
}

// Disconnect removes a peer from the neighborhood, it requests more peers if our outbound peer count is less than configured
func (s *swarm) Disconnect(key crypto.PublicKey) {
	peer := key.String()

	s.inpeersMutex.Lock()
	if _, ok := s.inpeers[peer]; ok {
		delete(s.inpeers, peer)
		s.inpeersMutex.Unlock()
		s.publishDelPeer(key)
		return
	}
	s.inpeersMutex.Unlock()

	s.outpeersMutex.Lock()
	if _, ok := s.outpeers[peer]; ok {
		delete(s.outpeers, peer)
	}
	s.outpeersMutex.Unlock()
	s.publishDelPeer(key)
	s.morePeersReq <- struct{}{}
}

// AddIncomingPeer inserts a peer to the neighborhood as a remote peer.
func (s *swarm) addIncomingPeer(n crypto.PublicKey) {
	s.inpeersMutex.Lock()
	// todo limit number of inpeers
	s.inpeers[n.String()] = n
	s.inpeersMutex.Unlock()
	s.publishNewPeer(n)
}

func (s *swarm) hasIncomingPeer(peer crypto.PublicKey) bool {
	s.inpeersMutex.RLock()
	_, ok := s.inpeers[peer.String()]
	s.inpeersMutex.RUnlock()
	return ok
}

func (s *swarm) hasOutgoingPeer(peer crypto.PublicKey) bool {
	s.outpeersMutex.RLock()
	_, ok := s.outpeers[peer.String()]
	s.outpeersMutex.RUnlock()
	return ok
}
