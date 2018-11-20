package p2p

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/proto"
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

type swarm struct {
	started   uint32
	bootErr   error
	bootChan  chan struct{}
	gossipErr error
	gossipC   chan struct{}

	config config.Config

	// set in construction and immutable state
	lNode *node.LocalNode

	// map between protocol names to listening protocol handlers
	// NOTE: maybe let more than one handler register on a protocol ?
	protocolHandlers     map[string]chan service.Message
	protocolHandlerMutex sync.RWMutex

	gossip gossip.Protocol

	network *net.Net

	cPool *connectionpool.ConnectionPool

	dht dht.DHT
	// Context for cancel
	ctx context.Context
	// Shutdown the loop
	shutdown chan struct{} // local request to kill the swarm from outside. e.g when local node is shutting down
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
		config:           config,
		lNode:            l,
		bootChan:         make(chan struct{}),
		gossipC:          make(chan struct{}),
		protocolHandlers: make(map[string]chan service.Message),
		network:          n,
		cPool:            connectionpool.NewConnectionPool(n, l.PublicKey()),
		shutdown:         make(chan struct{}), // non-buffered so requests to shutdown block until swarm is shut down
		ctx:              ctx,
	}

	s.dht = dht.New(l, config.SwarmConfig, s)

	s.gossip = gossip.NewNeighborhood(config.SwarmConfig, s.dht, s.cPool, s.lNode.Log)

	s.lNode.Debug("Created swarm for local node %s, %s", l.Address(), l.Pretty())

	return s, nil
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
				s.Shutdown()
			}
			close(s.bootChan)
			s.lNode.Info("DHT Bootstrapped with %d peers in %v", s.dht.Size(), time.Since(b))
		}()
	}

	go func() {
		if s.config.SwarmConfig.Bootstrap {
			s.waitForBoot()
		}
		err := s.gossip.Start()
		if err != nil {
			s.gossipErr = err
			s.Shutdown()
		}
		close(s.gossipC)
	}() // todo handle error async

	return nil
}

func (s *swarm) LocalNode() *node.LocalNode {
	return s.lNode
}

func (s *swarm) connectionPool() *connectionpool.ConnectionPool {
	return s.cPool
}

func (s *swarm) sendWrappedMessage(nodeID string, protocol string, payload *service.Data_MsgWrapper) error {
	return s.sendMessageImpl(nodeID, protocol, payload)
}

func (s *swarm) SendMessage(nodeID string, protocol string, payload []byte) error {
	return s.sendMessageImpl(nodeID, protocol, service.Data_Bytes{Payload: payload})
}

// SendMessage Sends a message to a remote node
// swarm will establish session if needed or use an existing session and open connection
// Designed to be used by any high level protocol
// req.reqID: globally unique id string - used for tracking messages we didn't get a response for yet
// req.msg: marshaled message data
// req.destId: receiver remote node public key/id
// Local request to send a message to a remote node
func (s *swarm) sendMessageImpl(peerPubKey string, protocol string, payload service.Data) error {
	s.lNode.Info("Sending message to %v", peerPubKey)
	var err error
	var peer node.Node
	var conn net.Connection

	peer, conn = s.gossip.Peer(peerPubKey) // check if he's a neighbor
	if peer == node.EmptyNode {
		peer, err = s.dht.Lookup(peerPubKey) // blocking, might issue a network lookup that'll take time.

		if err != nil {
			return err
		}
		conn, err = s.cPool.GetConnection(peer.Address(), peer.PublicKey()) // blocking, might take some time in case there is no connection
		if err != nil {
			s.lNode.Warning("failed to send message to %v, no valid connection. err: %v", peer.String(), err)
			return err
		}
	}

	session := conn.Session()
	if session == nil {
		s.lNode.Warning("failed to send message to %v, no valid session. err: %v", peer.String(), err)
		return err
	}

	protomessage := &pb.ProtocolMessage{
		Metadata: message.NewProtocolMessageMetadata(s.lNode.PublicKey(), protocol, false),
	}

	switch x := payload.(type) {
	case service.Data_MsgWrapper:
		protomessage.Data = &pb.ProtocolMessage_Msg{&pb.MessageWrapper{Type: x.MsgType, Req: x.Req, ReqID: x.ReqID, Payload: x.Payload}}
	case service.Data_Bytes:
		protomessage.Data = &pb.ProtocolMessage_Payload{Payload: x.Bytes()}
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
	newConnEvents := s.network.SubscribeOnNewRemoteConnections()
Loop:
	for {
		select {
		case nce := <-newConnEvents:
			go func(nod node.Node) { s.dht.Update(nod) }(nce.Node)
		case <-s.shutdown:
			break Loop
		}
	}
}

// swarm serial event processing
// provides concurrency safety as only one callback is executed at a time
// so there's no need for sync internal data structures
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
	// ErrNoPayload empty payload message
	ErrNoPayload = errors.New("deprecated code path, no payload in message")
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
		s.lNode.Fatal("Fatal error: Got nil message or connection")
		return ErrBadFormat1
	}

	s.lNode.Debug(fmt.Sprintf("Handle message from <<  %v", msg.Conn.RemotePublicKey().Pretty()))
	c := &pb.CommonMessageData{}
	err := proto.Unmarshal(msg.Message, c)
	if err != nil {
		return ErrBadFormat1
	}

	// check that the message was send within a reasonable time
	if ok := timesync.CheckMessageDrift(c.Timestamp); !ok {
		// TODO: consider kill connection with this node and maybe blacklist
		// TODO : Also consider moving send timestamp into metadata(encrypted).
		return ErrOutOfSync
	}

	if len(c.Payload) == 0 {
		return ErrNoPayload
	}

	// protocol messages are encrypted in payload
	// Locate the session
	session := msg.Conn.Session()

	if session == nil {
		return ErrNoSession
	}

	decPayload, err := session.Decrypt(c.Payload)
	if err != nil {
		return ErrFailDecrypt
	}

	pm := &pb.ProtocolMessage{}
	err = proto.Unmarshal(decPayload, pm)
	if err != nil {
		s.lNode.Errorf("proto marshinling err=", err)
		return ErrBadFormat2
	}
	if pm.Metadata == nil {
		spew.Dump(pm)
		panic("this is a defected message") // todo: Session bug, session scrambles messages and remove metadata
	}
	// authenticate message author - we already authenticated the sender via the shared session key secret
	err = message.AuthAuthor(pm)
	if err != nil {
		return ErrAuthAuthor
	}

	if !pm.Metadata.Gossip && !bytes.Equal(pm.Metadata.AuthPubKey, msg.Conn.RemotePublicKey().Bytes()) {
		//wtf ?
		return ErrNotFromPeer
	}

	s.lNode.Debug("Authorized %v protocol message ", pm.Metadata.Protocol)

	remoteNode := node.New(msg.Conn.RemotePublicKey(), "") // if we got so far, we already have the node in our rt, hence address won't be used
	// update the routing table - we just heard from this authenticated node
	s.dht.Update(remoteNode)

	// participate in gossip even if we don't know this protocol
	if pm.Metadata.Gossip { // todo : use gossip uid
		s.LocalNode().Debug("Got gossip message! relaying it")
		// don't block anyway
		err = s.gossip.Broadcast(decPayload) // err only if this is an old message
	}

	if err != nil {
		return nil
	}
	// route authenticated message to the reigstered protocol
	s.protocolHandlerMutex.RLock()
	msgchan := s.protocolHandlers[pm.Metadata.Protocol]
	s.protocolHandlerMutex.RUnlock()

	if msgchan == nil {
		s.LocalNode().Errorf("there was a bad protocol ", pm.Metadata.Protocol)
		return ErrNoProtocol
	}

	s.lNode.Debug("Forwarding message to protocol")

	if payload := pm.GetPayload(); payload != nil {
		msgchan <- protocolMessage{remoteNode, service.Data_Bytes{Payload: payload}}
	} else if wrap := pm.GetMsg(); wrap != nil {
		msgchan <- protocolMessage{remoteNode, service.Data_MsgWrapper{Req: wrap.Req, MsgType: wrap.Type, ReqID: wrap.ReqID, Payload: wrap.Payload}}
	}

	return nil
}

// Broadcast creates a gossip message signs it and disseminate it to neighbors
func (s *swarm) Broadcast(protocol string, payload []byte) error {
	// start by making the message
	pm := &pb.ProtocolMessage{
		Metadata: message.NewProtocolMessageMetadata(s.LocalNode().PublicKey(), protocol, true),
		Data:     &pb.ProtocolMessage_Payload{Payload: payload},
	}

	err := message.SignMessage(s.lNode.PrivateKey(), pm)
	if err != nil {
		return err
	}

	msg, err := proto.Marshal(pm)

	if err != nil {
		return err
	}

	return s.gossip.Broadcast(msg)
}
