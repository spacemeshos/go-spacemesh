package p2p

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/timesync"

	"encoding/hex"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
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

	network net.Net

	cPool *connectionpool.ConnectionPool

	dht *dht.DHT

	// Shutdown the loop
	shutdown chan struct{} // local request to kill the swarm from outside. e.g when local node is shutting down
}

// newSwarm creates a new P2P instance
func newSwarm(config config.Config, loadIdentity bool) (*swarm, error) {

	port := config.TCPPort
	address := fmt.Sprintf("0.0.0.0:%d", port)

	var l *node.LocalNode
	var err error
	// Load an existing identity from file if exists.

	if loadIdentity {
		l, err = node.NewLocalNode(config, address, true)
	} else {
		l, err = node.NewNodeIdentity(config, address, true)
	}
	if err != nil {
		log.Error("Failed to create a node, err: %v", err)
	}

	n, err := net.NewNet(config, l)
	if err != nil {
		log.Error("can't create swarm without a network", err)
		return nil, err
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

	go s.listenToNetworkMessages()

	if config.SwarmConfig.Bootstrap {
		err := s.dht.Bootstrap()
		if err != nil {
			s.Shutdown()
			return nil, err
		}
	}

	go s.checkTimeDrifts()

	return s, nil
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

func authAuthor(pm *pb.ProtocolMessage) error {
	sign := pm.Metadata.AuthorSign
	sPubkey := pm.Metadata.AuthPubKey

	pubkey, err := crypto.NewPublicKey(sPubkey)
	if err != nil {
		return fmt.Errorf("could'nt create public key from %v, err: %v", hex.EncodeToString(sPubkey), err)
	}

	pm.Metadata.AuthorSign = ""

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

	peer, err := s.dht.Lookup(peerPubKey)

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

func (s *swarm) Shutdown() {
	s.shutdown <- struct{}{}
}

func (s *swarm) shutDownInternal() {
	//TODO : Gracefully shutdown swarm => finish incmoing / outgoing msgs
	s.network.Shutdown()
}

func (s *swarm) listenToNetworkMessages() {
	go func() {
		for c := range s.network.SubscribeOnNewRemoteConnections() {
			s.dht.Update(node.New(c.RemotePublicKey(), c.RemoteAddr().String()))
		}
	}()
Loop:
	for {
		select {
		case m := <-s.network.IncomingMessages():
			go s.onRemoteClientMessage(m)

		case <-s.shutdown:
			s.shutdown <- struct{}{}
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
			s.shutDownInternal()
			s.shutdown <- struct{}{}
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

// onRemoteClientMessage pre-process a protocol message from a remote client handling decryption and authentication
// authenticated messages are forwarded to corresponding protocol handlers
// Main incoming network messages handler
// c: connection we got this message on
// msg: binary protobufs encoded data
func (s *swarm) onRemoteClientMessage(msg net.IncomingMessageEvent) {
	s.lNode.Debug(fmt.Sprintf("Handle message from <<  %v", msg.Conn.RemotePublicKey().Pretty()))
	c := &pb.CommonMessageData{}
	err := proto.Unmarshal(msg.Message, c)
	if err != nil {
		s.lNode.Warning("Bad request - closing connection...")
		msg.Conn.Close()
		return
	}

	// check that the message was send within a reasonable time
	if ok := timesync.CheckMessageDrift(c.Timestamp); !ok {
		// TODO: consider kill connection with this node and maybe blacklist
		// TODO : Also consider moving send timestamp into metadata.
		s.lNode.Error("Received out of sync msg from %s dropping .. ", msg.Conn.RemotePublicKey().Pretty())
		return
	}

	if len(c.Payload) == 0 {
		s.lNode.Error(" this code path is deprecated!!!")
		return
	}

	// protocol messages are encrypted in payload
	// Locate the session
	session := msg.Conn.Session()

	decPayload, err := session.Decrypt(c.Payload)
	if err != nil {
		s.lNode.Warning("Dropping incoming protocol message - can't decrypt message payload with session key. %v", err)
		return
	}

	pm := &pb.ProtocolMessage{}
	err = proto.Unmarshal(decPayload, pm)
	if err != nil {
		s.lNode.Warning("Dropping incoming protocol message - Failed to get protocol message from payload. %v", err)
		return
	}

	// authenticate message author - we already authenticated the sender via the shared session key secret
	err = authAuthor(pm)
	if err != nil {
		s.lNode.Warning("Dropping incoming protocol message - Failed to verify author. %v", err)
		return
	}

	s.lNode.Debug("Authorized %v protocol message ", pm.Metadata.Protocol)

	remoteNode := node.New(msg.Conn.RemotePublicKey(), msg.Conn.RemoteAddr().String())
	// update the routing table - we just heard from this authenticated node
	s.dht.Update(remoteNode)
	// route authenticated message to the reigstered protocol
	s.protocolHandlerMutex.RLock()
	msgchan := s.protocolHandlers[pm.Metadata.Protocol]
	s.protocolHandlerMutex.RUnlock()

	if msgchan == nil {
		s.lNode.Errorf("Received msg to an unsupported protocol")
		return
	}

	s.lNode.Debug("Forwarding message to protocol")
	msgchan <- protocolMessage{remoteNode, pm.Payload}
}
