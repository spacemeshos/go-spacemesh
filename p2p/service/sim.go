package service

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/rand"
)

// TODO : implement delays?

// Simulator is a p2p node factory and message bridge that is used to simulate the p2p layer provided
// to protocols without using real network or p2p code. It resembles the `Service` interface API.
type Simulator struct {
	io.Closer
	mutex                 sync.RWMutex
	protocolDirectHandler map[p2pcrypto.PublicKey]map[string]chan DirectMessage // maps peerPubkey -> protocol -> direct protocol handler
	protocolGossipHandler map[p2pcrypto.PublicKey]map[string]chan GossipMessage // maps peerPubkey -> protocol -> gossip protocol handler
	nodes                 map[p2pcrypto.PublicKey]*Node

	subLock      sync.Mutex
	newPeersSubs []chan p2pcrypto.PublicKey
	delPeersSubs []chan p2pcrypto.PublicKey
}

var _ Service = new(Node)

// Node is a simulated p2p node that can be used as a p2p service
type Node struct {
	sim *Simulator
	*node.Info
	sndDelay      uint32
	rcvDelay      uint32
	randBehaviour bool
}

// NewSimulator Creates a p2p simulation by providing nodes as p2p services and bridge them.
func NewSimulator() *Simulator {
	s := &Simulator{
		protocolDirectHandler: make(map[p2pcrypto.PublicKey]map[string]chan DirectMessage),
		protocolGossipHandler: make(map[p2pcrypto.PublicKey]map[string]chan GossipMessage),
		nodes:                 make(map[p2pcrypto.PublicKey]*Node),
	}
	return s
}

// SubscribeToPeerEvents starts listening to new peers and disconnected peers events.
func (s *Simulator) SubscribeToPeerEvents(myid p2pcrypto.Key) (chan p2pcrypto.PublicKey, chan p2pcrypto.PublicKey) {
	s.mutex.RLock()
	var keys []p2pcrypto.PublicKey
	for _, nd := range s.nodes {
		if nd.PublicKey() == myid {
			continue
		}
		keys = append(keys, nd.PublicKey())
	}
	s.mutex.RUnlock()
	newp := make(chan p2pcrypto.PublicKey, len(keys)+1)
	delp := make(chan p2pcrypto.PublicKey, len(keys)+1)
	s.subLock.Lock()
	s.newPeersSubs = append(s.newPeersSubs, newp)
	s.delPeersSubs = append(s.delPeersSubs, delp)
	s.subLock.Unlock()
	for _, x := range keys {
		newp <- x
	}
	return newp, delp
}

func (s *Simulator) publishNewPeer(peer p2pcrypto.PublicKey) {
	s.subLock.Lock()
	for _, ch := range s.newPeersSubs {
		log.Info("publish on chan with len %v, cap %v", len(ch), cap(ch))
		ch <- peer
	}
	s.subLock.Unlock()
}

// nolint
func (s *Simulator) publishDelPeer(peer p2pcrypto.PublicKey) {
	s.subLock.Lock()
	for _, ch := range s.delPeersSubs {
		ch <- peer
	}
	s.subLock.Unlock()
}

func (s *Simulator) createdNode(n *Node) {
	s.mutex.Lock()
	s.protocolDirectHandler[n.PublicKey()] = make(map[string]chan DirectMessage)
	s.protocolGossipHandler[n.PublicKey()] = make(map[string]chan GossipMessage)
	s.nodes[n.PublicKey()] = n
	s.mutex.Unlock()
	s.publishNewPeer(n.PublicKey())
}

// NewFaulty creates a node that can deploy faulty behaviour, broadcast delay, receive delay and randomness.
func (s *Simulator) NewFaulty(isRandBehaviour bool, maxBroadcastDelaySec uint32, maxReceiveDelaySec uint32) *Node {
	n := s.NewNode()
	n.randBehaviour = isRandBehaviour
	n.sndDelay = maxBroadcastDelaySec
	n.rcvDelay = maxReceiveDelaySec

	return n
}

// NewNode creates a new p2p node in this Simulator
func (s *Simulator) NewNode() *Node {
	n := node.GenerateRandomNodeData()
	sn := &Node{
		sim:  s,
		Info: n,
	}
	s.createdNode(sn)
	return sn
}

// NewNodeFrom creates a new node from existing details
func (s *Simulator) NewNodeFrom(n *node.Info) *Node {
	sn := &Node{
		sim:  s,
		Info: n,
	}
	s.createdNode(sn)
	return sn
}

type simDirectMessage struct {
	metadata P2PMetadata
	msg      Data
	sender   p2pcrypto.PublicKey
}

func (sm simDirectMessage) Metadata() P2PMetadata {
	return sm.metadata
}

// Bytes is the message's binary data in byte array format.
func (sm simDirectMessage) Data() Data {
	return sm.msg
}

// Bytes is the message's binary data in byte array format.
func (sm simDirectMessage) Bytes() []byte {
	return sm.msg.Bytes()
}

// Sender is the node who sent this message
func (sm simDirectMessage) Sender() p2pcrypto.PublicKey {
	return sm.sender
}

type simGossipMessage struct {
	sender                  p2pcrypto.PublicKey
	msg                     Data
	validationCompletedChan chan MessageValidation
}

// Bytes is the message's binary data in byte array format.
func (sm simGossipMessage) Data() Data {
	return sm.msg
}

// Sender
func (sm simGossipMessage) Sender() p2pcrypto.PublicKey {
	return sm.sender
}

// Bytes is the message's binary data in byte array format.
func (sm simGossipMessage) Bytes() []byte {
	return sm.msg.Bytes()
}

// ValidationCompletedChan is a channel over which the protocol is expected to update on the message validation
func (sm simGossipMessage) ValidationCompletedChan() chan MessageValidation {
	return sm.validationCompletedChan
}

// ReportValidation reports sm as a valid message for protocol.
func (sm simGossipMessage) ReportValidation(protocol string) {
	if sm.validationCompletedChan != nil {
		sm.validationCompletedChan <- NewMessageValidation(sm.sender, sm.Bytes(), protocol)
	}
}

// Start is here to satisfy the Service interface.
func (sn *Node) Start() error {
	// on simulation this doesn't really matter yet.
	return nil
}

// simulator doesn't go through the regular p2p pipes so the metadata won't be available.
// it's okay since this data doesn't matter to the simulator
func simulatorMetadata() P2PMetadata {
	ip, err := net.ResolveTCPAddr("tcp", "127.0.0.1:1234")
	if err != nil {
		panic("simulator error")
	}
	return P2PMetadata{ip}
}

// ProcessDirectProtocolMessage passes a direct message to the protocol.
func (sn *Node) ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, payload Data, metadata P2PMetadata) error {
	//sn.sleep(sn.rcvDelay)
	sn.sim.mutex.RLock()
	c, ok := sn.sim.protocolDirectHandler[sn.PublicKey()][protocol]
	sn.sim.mutex.RUnlock()
	if !ok {
		return errors.New("unknown protocol")
	}
	c <- simDirectMessage{simulatorMetadata(), payload, sender}
	return nil
}

// ProcessGossipProtocolMessage passes a gossip message to the protocol.
func (sn *Node) ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data Data, validationCompletedChan chan MessageValidation) error {
	sn.sim.mutex.RLock()
	c, ok := sn.sim.protocolGossipHandler[sn.PublicKey()][protocol]
	sn.sim.mutex.RUnlock()
	if !ok {
		return errors.New("unknown protocol")
	}
	c <- simGossipMessage{sender, data, validationCompletedChan}
	return nil
}

// SendMessage sends a protocol message to the specified nodeID.
// returns error if the node cant be found. corresponds to `SendMessage`

// SendWrappedMessage send a wrapped message to another simulated node.
func (sn *Node) SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *DataMsgWrapper) error {
	return sn.sendMessageImpl(nodeID, protocol, payload)
}

// SendMessage send a message to another simulated node.
func (sn *Node) SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	return sn.sendMessageImpl(peerPubkey, protocol, DataBytes{Payload: payload})
}

// GossipReady is a chan which is closed when we established initial min connections with peers.
func (sn *Node) GossipReady() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func (sn *Node) sendMessageImpl(nodeID p2pcrypto.PublicKey, protocol string, payload Data) error {
	sn.sim.mutex.RLock()
	thec, ok := sn.sim.protocolDirectHandler[nodeID][protocol]
	sn.sim.mutex.RUnlock()
	if ok {
		thec <- simDirectMessage{simulatorMetadata(), payload, sn.Info.PublicKey()}
		return nil
	}
	return errors.New("could not find " + protocol + " handler for node: " + nodeID.String())
}

func (sn *Node) sleep(delay uint32) {
	if delay == 0 {
		return
	}

	ranDelay := delay
	if sn.randBehaviour {
		ranDelay = rand.Uint32() % delay
	}
	time.Sleep(time.Second * time.Duration(ranDelay))
}

// Broadcast disseminates a message to all simulated nodes. sends to yourself first.
func (sn *Node) Broadcast(protocol string, payload []byte) error {
	go func() {
		sn.sleep(sn.sndDelay)
		sn.sim.mutex.Lock()
		var mychan chan GossipMessage

		if me, ok := sn.sim.protocolGossipHandler[sn.PublicKey()][protocol]; ok {
			mychan = me
		}

		sendees := make([]chan GossipMessage, 0, len(sn.sim.protocolGossipHandler))

		for n := range sn.sim.protocolGossipHandler {
			if n == sn.PublicKey() {
				continue
			}
			if c, ok := sn.sim.protocolGossipHandler[n][protocol]; ok {
				sendees = append(sendees, c) // <- simGossipMessage{sn.Info.PublicKey(), DataBytes{Payload: payload}, nil}
			}
		}
		sn.sim.mutex.Unlock()

		if mychan != nil {
			mychan <- simGossipMessage{sn.Info.PublicKey(), DataBytes{Payload: payload}, nil}
		}

		for _, c := range sendees {
			c <- simGossipMessage{sn.Info.PublicKey(), DataBytes{Payload: payload}, nil}
		}

		log.Debug("%v >> All ( Gossip ) (%v)", sn.Info.PublicKey(), payload)
	}()
	return nil
}

// SubscribePeerEvents satisfy the Service interface and registers channels for new simulator peers joining.
func (sn *Node) SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey) {
	return sn.sim.SubscribeToPeerEvents(sn.PublicKey())
}

// RegisterDirectProtocol creates and returns a channel for a given direct based protocol.
func (sn *Node) RegisterDirectProtocol(protocol string) chan DirectMessage {
	c := make(chan DirectMessage, 1000)
	sn.sim.mutex.Lock()
	sn.sim.protocolDirectHandler[sn.Info.PublicKey()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
}

// RegisterGossipProtocol creates and returns a channel for a given gossip based protocol.
func (sn *Node) RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan GossipMessage {
	c := make(chan GossipMessage, 1000)
	sn.sim.mutex.Lock()
	sn.sim.protocolGossipHandler[sn.Info.PublicKey()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
}

// RegisterDirectProtocolWithChannel configures and returns a channel for a given protocol.
func (sn *Node) RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan DirectMessage) chan DirectMessage {
	sn.sim.mutex.Lock()
	sn.sim.protocolDirectHandler[sn.Info.PublicKey()][protocol] = ingressChannel
	sn.sim.mutex.Unlock()
	return ingressChannel
}

// Shutdown closes all node channels are remove it from the Simulator map
func (sn *Node) Shutdown() {
	sn.sim.mutex.Lock()
	// TODO: close all chans, but that makes us send on nil chan.
	delete(sn.sim.protocolDirectHandler, sn.Info.PublicKey())
	delete(sn.sim.protocolGossipHandler, sn.Info.PublicKey())
	sn.sim.mutex.Unlock()

	sn.sim.subLock.Lock()
	for _, ch := range sn.sim.newPeersSubs {
		close(ch)
	}
	sn.sim.newPeersSubs = nil
	for _, ch := range sn.sim.delPeersSubs {
		close(ch)
	}
	sn.sim.delPeersSubs = nil
	sn.sim.subLock.Unlock()
}
