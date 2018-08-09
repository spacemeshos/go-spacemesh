package p2p

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/timesync"

	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"strings"
	"sync"
)

type protocolMessage struct {
	sender node.Node
	data   []byte
}

func (pm protocolMessage) Sender() node.Node {
	return pm.sender
}

func (pm protocolMessage) Data() []byte {
	return pm.data
}

type swarm struct {
	config config.Config

	// set in construction and immutable state
	lNode *node.LocalNode

	// map between protocol names to listening protocol handlers
	// NOTE: maybe let more than one handler register on a protocol ?
	protocolHandlers     map[string]chan service.Message
	protocolHandlerMutex sync.RWMutex

	network *net.Net

	cPool *connectionpool.ConnectionPool

	dht dht.DHT

	// Shutdown the loop
	shutdown chan struct{} // local request to kill the swarm from outside. e.g when local node is shutting down
}

// newSwarm creates a new P2P instance
func newSwarm(config config.Config, newNode bool) (*swarm, error) {

	port := config.TCPPort
	address := fmt.Sprintf("127.0.0.1:%d", port)

	var l *node.LocalNode
	var err error
	// Load an existing identity from file if exists.

	if newNode {
		l, err = node.NewNodeIdentity(config, address, true)
	} else {
		l, err = node.NewLocalNode(config, address, true)
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
		protocolHandlers: make(map[string]chan service.Message),
		network:          n,
		cPool:            connectionpool.NewConnectionPool(n, l.PublicKey()),
		shutdown:         make(chan struct{}), // non-buffered so requests to shutdown block until swarm is shut down

	}

	s.dht = dht.New(l, config.SwarmConfig, s)

	s.lNode.Debug("Created swarm for local node %s, %s", l.Address(), l.Pretty())



	return s, nil
}

func (s *swarm) Start() error{
	go s.listenToNetworkMessages()

	if s.config.SwarmConfig.Bootstrap {
		err := s.dht.Bootstrap()
		if err != nil {
			s.Shutdown()
			return err
		}
	}

	go s.checkTimeDrifts()
	return nil
}

// newProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func newProtocolMessageMetadata(author crypto.PublicKey, protocol string, gossip bool) *pb.Metadata {
	return &pb.Metadata{
		Protocol:      protocol,
		ClientVersion: config.ClientVersion,
		Timestamp:     time.Now().Unix(),
		Gossip:        gossip,
		AuthPubKey:    author.Bytes(),
	}
}

func (s *swarm) localNode() *node.LocalNode {
	return s.lNode
}

func (s *swarm) connectionPool() *connectionpool.ConnectionPool {
	return s.cPool
}

func signMessage(pv crypto.PrivateKey, pm *pb.ProtocolMessage) error {
	data, err := proto.Marshal(pm)
	if err != nil {
		e := fmt.Errorf("invalid msg format %v", err)
		return e
	}

	sign, err := pv.Sign(data)
	if err != nil {
		return fmt.Errorf("failed to sign message err:%v", err)
	}

	// TODO : AuthorSign: string => bytes
	pm.Metadata.AuthorSign = hex.EncodeToString(sign)

	return nil
}

// authAuthor authorizes that a message is signed by its claimed author
func authAuthor(pm *pb.ProtocolMessage) error {
	// TODO: consider getting pubkey from outside. attackar coul'd just manipulate the whole message pubkey and sign.
	sign := pm.Metadata.AuthorSign
	sPubkey := pm.Metadata.AuthPubKey

	pubkey, err := crypto.NewPublicKey(sPubkey)
	if err != nil {
		return fmt.Errorf("could'nt create public key from %v, err: %v", hex.EncodeToString(sPubkey), err)
	}

	pm.Metadata.AuthorSign = "" // we have to verify the message without the sign

	bin, err := proto.Marshal(pm)

	if err != nil {
		return err
	}

	binsig, err := hex.DecodeString(sign)
	if err != nil {
		return err
	}

	v, err := pubkey.Verify(bin, binsig)

	if err != nil {
		return err
	}

	if !v {
		return fmt.Errorf("coudld'nt verify message")
	}

	pm.Metadata.AuthorSign = sign // restore sign because maybe we'll send it again ( gossip )

	return nil
}

// SendMessage Sends a message to a remote node
// swarm will establish session if needed or use an existing session and open connection
// Designed to be used by any high level protocol
// req.reqID: globally unique id string - used for tracking messages we didn't get a response for yet
// req.msg: marshaled message data
// req.destId: receiver remote node public key/id
// Local request to send a message to a remote node
func (s *swarm) SendMessage(peerPubKey string, protocol string, payload []byte) error {

	peer, err := s.dht.Lookup(peerPubKey) // blocking, might issue a network lookup that'll take time.

	if err != nil {
		return err
	}

	conn, err := s.cPool.GetConnection(peer.Address(), peer.PublicKey()) // blocking, might take some time in case there is no connection
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
		Metadata: newProtocolMessageMetadata(s.lNode.PublicKey(), protocol, false),
		Payload:  payload,
	}

	err = signMessage(s.lNode.PrivateKey(), protomessage)
	if err != nil {
		return err
	}

	err = authAuthor(protomessage)
	if err != nil {
		return err
	}

	data, err := proto.Marshal(protomessage)
	if err != nil {
		return fmt.Errorf("failed to encode signed message err: %v", err)
	}

	encPayload, err := session.Encrypt(data)
	if err != nil {
		e := fmt.Errorf("aborting send - failed to encrypt payload: %v", err)
		return e
	}

	cmd := &pb.CommonMessageData{
		SessionId: session.ID(),
		Payload:   encPayload,
		Timestamp: time.Now().Unix(),
	}

	final, err := proto.Marshal(cmd)
	if err != nil {
		e := fmt.Errorf("aborting send - invalid msg format %v", err)
		return e
	}

	// finally - send it away!
	s.lNode.Debug("Sending protocol message down the connection to %v", log.PrettyID(peerPubKey))

	return conn.Send(final)
}

// RegisterProtocol registers an handler for `protocol`
func (s *swarm) RegisterProtocol(protocol string) chan service.Message {
	mchan := make(chan service.Message)
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
	err := s.onRemoteClientMessage(ime)
	if err != nil {
		ime.Conn.Close()
		// TODO: differentiate action on errors
	}
}

// update a full connection to the routing table.
func (s *swarm) updateConnection(nc net.Connection) {
	if nc.RemotePublicKey() != nil {
		s.dht.Update(node.New(nc.RemotePublicKey(), fmt.Sprintf("%v:%v", strings.Split(nc.RemoteAddr().String(), ":")[0], s.config.TCPPort)))
	}
}

// listenToNetworkMessages is waiting for network events from net as new connections or messages and handles them.
func (s *swarm) listenToNetworkMessages() {
	newconnections := s.network.SubscribeOnNewRemoteConnections()
Loop:
	for {
		select {
		case ime := <-s.network.IncomingMessages():
			go s.processMessage(ime)
		case nc := <-newconnections:
			go s.updateConnection(nc)
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
	// ErrBadFormat could'nt deserialize
	ErrBadFormat = errors.New("bad msg format, could'nt deserialize")
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
)

// onRemoteClientMessage pre-process a protocol message from a remote client handling decryption and authentication
// authenticated messages are forwarded to corresponding protocol handlers
// Main incoming network messages handler
// c: connection we got this message on
// msg: binary protobufs encoded data
func (s *swarm) onRemoteClientMessage(msg net.IncomingMessageEvent) error {
	s.lNode.Debug(fmt.Sprintf("Handle message from <<  %v", msg.Conn.RemotePublicKey().Pretty()))
	c := &pb.CommonMessageData{}
	err := proto.Unmarshal(msg.Message, c)
	if err != nil {
		return ErrBadFormat
	}

	// check that the message was send within a reasonable time
	if ok := timesync.CheckMessageDrift(c.Timestamp); !ok {
		// TODO: consider kill connection with this node and maybe blacklist
		// TODO : Also consider moving send timestamp into metadata.
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
		return ErrBadFormat
	}

	// authenticate message author - we already authenticated the sender via the shared session key secret
	err = authAuthor(pm)
	if err != nil {
		return ErrAuthAuthor
	}

	s.lNode.Debug("Authorized %v protocol message ", pm.Metadata.Protocol)

	remoteNode := node.New(msg.Conn.RemotePublicKey(), fmt.Sprintf("%v:%v", strings.Split(msg.Conn.RemoteAddr().String(), ":")[0], s.config.TCPPort))
	// update the routing table - we just heard from this authenticated node
	s.dht.Update(remoteNode)
	// route authenticated message to the reigstered protocol
	s.protocolHandlerMutex.RLock()
	msgchan := s.protocolHandlers[pm.Metadata.Protocol]
	s.protocolHandlerMutex.RUnlock()

	if msgchan == nil {
		return ErrNoProtocol
	}

	s.lNode.Debug("Forwarding message to protocol")
	msgchan <- protocolMessage{remoteNode, pm.Payload}

	return nil
}
