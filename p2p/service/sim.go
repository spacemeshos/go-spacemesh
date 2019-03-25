package service

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
	"io"
	"net"
	"sync"
	"time"
)

// TODO : implement delays?

// Simulator is a p2p node factory and message bridge
type Simulator struct {
	io.Closer
	mutex                 sync.RWMutex
	protocolDirectHandler map[string]map[string]chan DirectMessage // maps peerPubkey -> protocol -> direct protocol handler
	protocolGossipHandler map[string]map[string]chan GossipMessage // maps peerPubkey -> protocol -> gossip protocol handler
	nodes                 map[string]*Node

	subLock      sync.Mutex
	newPeersSubs []chan p2pcrypto.PublicKey
	delPeersSubs []chan p2pcrypto.PublicKey
}

var _ Service = new(Node)

type dht interface {
	Update(node2 node.Node)
}

// Node is a simulated p2p node that can be used as a p2p service
type Node struct {
	sim *Simulator
	node.Node
	dht           dht
	sndDelay      uint32
	rcvDelay      uint32
	randBehaviour bool
}

// New Creates a p2p simulation by providing nodes as p2p services and bridge them.
func NewSimulator() *Simulator {
	s := &Simulator{
		protocolDirectHandler: make(map[string]map[string]chan DirectMessage),
		protocolGossipHandler: make(map[string]map[string]chan GossipMessage),
		nodes:                 make(map[string]*Node),
	}
	return s
}

func (s *Simulator) SubscribeToPeerEvents() (chan p2pcrypto.PublicKey, chan p2pcrypto.PublicKey) {
	newp := make(chan p2pcrypto.PublicKey)
	delp := make(chan p2pcrypto.PublicKey)
	s.subLock.Lock()
	s.newPeersSubs = append(s.newPeersSubs, newp)
	s.delPeersSubs = append(s.delPeersSubs, delp)
	s.subLock.Unlock()
	return newp, delp
}

func (s *Simulator) publishNewPeer(peer p2pcrypto.PublicKey) {
	s.subLock.Lock()
	for _, ch := range s.newPeersSubs {
		ch <- peer
	}
	s.subLock.Unlock()
}

func (s *Simulator) publishDelPeer(peer p2pcrypto.PublicKey) {
	s.subLock.Lock()
	for _, ch := range s.delPeersSubs {
		ch <- peer
	}
	s.subLock.Unlock()
}

func (s *Simulator) createdNode(n *Node) {
	s.mutex.Lock()
	s.protocolDirectHandler[n.PublicKey().String()] = make(map[string]chan DirectMessage)
	s.protocolGossipHandler[n.PublicKey().String()] = make(map[string]chan GossipMessage)
	s.nodes[n.PublicKey().String()] = n
	s.mutex.Unlock()
	s.publishNewPeer(n.PublicKey())
}

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
		Node: n,
	}
	s.createdNode(sn)
	return sn
}

// NewNodeFrom creates a new node from existing details
func (s *Simulator) NewNodeFrom(n node.Node) *Node {
	sn := &Node{
		sim:  s,
		Node: n,
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
	msg                     Data
	validationCompletedChan chan MessageValidation
}

// Bytes is the message's binary data in byte array format.
func (sm simGossipMessage) Data() Data {
	return sm.msg
}

// Bytes is the message's binary data in byte array format.
func (sm simGossipMessage) Bytes() []byte {
	return sm.msg.Bytes()
}

// ValidationCompletedChan is a channel over which the protocol is expected to update on the message validation
func (sm simGossipMessage) ValidationCompletedChan() chan MessageValidation {
	return sm.validationCompletedChan
}

func (sm simGossipMessage) ReportValidation(protocol string, isValid bool) {
	if sm.validationCompletedChan != nil {
		sm.validationCompletedChan <- NewMessageValidation(sm.Bytes(), protocol, isValid)
	}
}

func (sn *Node) Start() error {
	// on simulation this doesn't really matter yet.
	return nil
}

// simulator doesn't go through the regular p2p pipes so the metadata won't be available.
// it's okay since this data doesn't matter to the simulator
func simulatorMetadata() P2PMetadata {
	ip, err := net.ResolveIPAddr("ip", "0.0.0.0")
	if err != nil {
		log.Panic("cant resolve local ip")
	}
	return P2PMetadata{ip}
}

// ProcessDirectProtocolMessage
func (sn *Node) ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, payload Data, metadata P2PMetadata) error {
	sn.sleep(sn.rcvDelay)
	sn.sim.mutex.RLock()
	c, ok := sn.sim.protocolDirectHandler[sn.PublicKey().String()][protocol]
	sn.sim.mutex.RUnlock()
	if !ok {
		return errors.New("Unknown protocol")
	}
	c <- simDirectMessage{simulatorMetadata(), payload, sender}
	return nil
}

// ProcessGossipProtocolMessage
func (sn *Node) ProcessGossipProtocolMessage(protocol string, payload Data, validationCompletedChan chan MessageValidation) error {
	sn.sim.mutex.RLock()
	c, ok := sn.sim.protocolGossipHandler[sn.PublicKey().String()][protocol]
	sn.sim.mutex.RUnlock()
	if !ok {
		return errors.New("Unknown protocol")
	}
	c <- simGossipMessage{payload, validationCompletedChan}
	return nil
}

// SendMessage sends a protocol message to the specified nodeID.
// returns error if the node cant be found. corresponds to `SendMessage`

func (sn *Node) SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *DataMsgWrapper) error {
	return sn.sendMessageImpl(nodeID, protocol, payload)
}

func (sn *Node) SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	return sn.sendMessageImpl(peerPubkey, protocol, DataBytes{Payload: payload})
}

func (sn *Node) sendMessageImpl(nodeID p2pcrypto.PublicKey, protocol string, payload Data) error {
	sn.sim.mutex.RLock()
	thec, ok := sn.sim.protocolDirectHandler[nodeID.String()][protocol]
	sn.sim.mutex.RUnlock()
	if ok {
		thec <- simDirectMessage{simulatorMetadata(), payload, sn.Node.PublicKey()}
		return nil
	}
	log.Debug("%v >> %v (%v)", sn.Node.PublicKey(), nodeID, payload)
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

// Broadcast
func (sn *Node) Broadcast(protocol string, payload []byte) error {
	go func() {
		sn.sleep(sn.sndDelay)
		sn.sim.mutex.RLock()
		for n := range sn.sim.protocolGossipHandler {
			if c, ok := sn.sim.protocolGossipHandler[n][protocol]; ok {
				c <- simGossipMessage{DataBytes{Payload: payload}, nil}
			}
		}
		sn.sim.mutex.RUnlock()
		log.Debug("%v >> All ( Gossip ) (%v)", sn.Node.PublicKey(), payload)
	}()
	return nil
}

func (sn *Node) SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey) {
	return sn.sim.SubscribeToPeerEvents()
}

// RegisterDirectProtocol creates and returns a channel for a given direct based protocol.
func (sn *Node) RegisterDirectProtocol(protocol string) chan DirectMessage {
	c := make(chan DirectMessage)
	sn.sim.mutex.Lock()
	sn.sim.protocolDirectHandler[sn.Node.PublicKey().String()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
}

// RegisterGossipProtocol creates and returns a channel for a given gossip based protocol.
func (sn *Node) RegisterGossipProtocol(protocol string) chan GossipMessage {
	c := make(chan GossipMessage)
	sn.sim.mutex.Lock()
	sn.sim.protocolGossipHandler[sn.Node.PublicKey().String()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
}

// RegisterProtocolWithChannel configures and returns a channel for a given protocol.
func (sn *Node) RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan DirectMessage) chan DirectMessage {
	sn.sim.mutex.Lock()
	sn.sim.protocolDirectHandler[sn.Node.String()][protocol] = ingressChannel
	sn.sim.mutex.Unlock()
	return ingressChannel
}

// AttachDHT attaches a dht for the update function of the simulation node
func (sn *Node) AttachDHT(dht dht) {
	sn.dht = dht
}

// Shutdown closes all node channels are remove it from the Simulator map
func (sn *Node) Shutdown() {
	sn.sim.mutex.Lock()
	for _, c := range sn.sim.protocolDirectHandler[sn.Node.PublicKey().String()] {
		close(c)
	}
	delete(sn.sim.protocolDirectHandler, sn.Node.PublicKey().String())

	for _, c := range sn.sim.protocolGossipHandler[sn.Node.PublicKey().String()] {
		close(c)
	}
	delete(sn.sim.protocolGossipHandler, sn.Node.PublicKey().String())
	sn.sim.mutex.Unlock()
}
