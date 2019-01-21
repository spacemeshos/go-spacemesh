package service

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"io"
	"sync"
)

// TODO : implmement delays?

// Simulator is a p2p node factory and message bridge
type Simulator struct {
	io.Closer
	mutex           sync.RWMutex
	protocolHandler map[string]map[string]chan Message // maps peerPubkey -> protocol -> handler
	nodes           map[string]*Node

	subLock      sync.Mutex
	newPeersSubs []chan crypto.PublicKey
	delPeersSubs []chan crypto.PublicKey
}

var _ Service = new(Node)

type dht interface {
	Update(node2 node.Node)
}

// Node is a simulated p2p node that can be used as a p2p service
type Node struct {
	sim *Simulator
	node.Node
	dht dht
}

// New Creates a p2p simulation by providing nodes as p2p services and bridge them.
func NewSimulator() *Simulator {
	s := &Simulator{
		protocolHandler: make(map[string]map[string]chan Message),
		nodes:           make(map[string]*Node),
	}
	return s
}

func (s *Simulator) SubscribeToPeerEvents() (chan crypto.PublicKey, chan crypto.PublicKey) {
	newp := make(chan crypto.PublicKey)
	delp := make(chan crypto.PublicKey)
	s.subLock.Lock()
	s.newPeersSubs = append(s.newPeersSubs, newp)
	s.delPeersSubs = append(s.delPeersSubs, delp)
	s.subLock.Unlock()
	return newp, delp
}

func (s *Simulator) publishNewPeer(peer crypto.PublicKey) {
	s.subLock.Lock()
	for _, s := range s.newPeersSubs {
		s <- peer
	}
	s.subLock.Unlock()
}

func (s *Simulator) publishDelPeer(peer crypto.PublicKey) {
	s.subLock.Lock()
	for _, s := range s.delPeersSubs {
		s <- peer
	}
	s.subLock.Unlock()
}

func (s *Simulator) createdNode(n *Node) {
	s.mutex.Lock()
	s.protocolHandler[n.PublicKey().String()] = make(map[string]chan Message)
	s.nodes[n.PublicKey().String()] = n
	s.mutex.Unlock()
	s.publishNewPeer(n.PublicKey())
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

func (s *Simulator) updateNode(node string, sender *Node) {
	s.mutex.Lock()
	n, ok := s.nodes[node]
	if ok {
		if n.dht != nil {
			n.Update(sender.Node)
		}
	}
	s.mutex.Unlock()
}

type simMessage struct {
	msg    Data
	sender node.Node
}

// Bytes is the message's binary data in byte array format.
func (sm simMessage) Data() Data {
	return sm.msg
}

// Bytes is the message's binary data in byte array format.
func (sm simMessage) Bytes() []byte {
	return sm.msg.Bytes()
}

// Sender is the node who sent this message
func (sm simMessage) Sender() node.Node {
	return sm.sender
}

func (sn *Node) Start() error {
	// on simulation this doesn't really matter yet.
	return nil
}

// ProcessProtocolMessage
func (sn *Node) ProcessProtocolMessage(sender node.Node, protocol string, payload Data) error {
	sn.sim.mutex.RLock()
	c, ok := sn.sim.protocolHandler[sn.String()][protocol]
	sn.sim.mutex.RUnlock()
	if !ok {
		return errors.New("Unknown protocol")
	}
	c <- simMessage{payload, sender}
	return nil
}

// SendMessage sends a protocol message to the specified nodeID.
// returns error if the node cant be found. corresponds to `SendMessage`

func (s *Node) SendWrappedMessage(nodeID string, protocol string, payload *DataMsgWrapper) error {
	return s.sendMessageImpl(nodeID, protocol, payload)
}

func (s *Node) SendMessage(nodeID string, protocol string, payload []byte) error {
	return s.sendMessageImpl(nodeID, protocol, DataBytes{Payload: payload})
}

func (sn *Node) sendMessageImpl(nodeID string, protocol string, payload Data) error {
	sn.sim.mutex.RLock()
	thec, ok := sn.sim.protocolHandler[nodeID][protocol]
	sn.sim.mutex.RUnlock()
	if ok {
		thec <- simMessage{payload, sn.Node}
		sn.sim.updateNode(nodeID, sn)
		return nil
	}
	log.Debug("%v >> %v (%v)", sn.Node.PublicKey(), nodeID, payload)
	return errors.New("could not find " + protocol + " handler for node: " + nodeID)
}

// Broadcast
func (sn *Node) Broadcast(protocol string, payload []byte) error {
	go func() {
		sn.sim.mutex.RLock()
		for n := range sn.sim.protocolHandler {
			if c, ok := sn.sim.protocolHandler[n][protocol]; ok {
				c <- simMessage{DataBytes{Payload: payload}, sn.Node}
			}
		}
		sn.sim.mutex.RUnlock()
		log.Debug("%v >> All ( Gossip ) (%v)", sn.Node.PublicKey(), payload)
	}()
	return nil
}

func (sn *Node) SubscribePeerEvents() (chan crypto.PublicKey, chan crypto.PublicKey) {
	return sn.sim.SubscribeToPeerEvents()
}

// RegisterProtocol creates and returns a channel for a given protocol.
func (sn *Node) RegisterProtocol(protocol string) chan Message {
	c := make(chan Message)
	sn.sim.mutex.Lock()
	sn.sim.protocolHandler[sn.Node.String()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
}

// AttachDHT attaches a dht for the update function of the simulation node
func (sn *Node) AttachDHT(dht dht) {
	sn.dht = dht
}

// Update updates a node in the dht, it panics if no dht was declared
func (sn *Node) Update(node2 node.Node) {
	if sn.dht == nil {
		panic("Tried to update without attaching dht")
	}
	sn.dht.Update(node2)
}

// Shutdown closes all node channels are remove it from the Simulator map
func (sn *Node) Shutdown() {
	sn.sim.mutex.Lock()
	for _, c := range sn.sim.protocolHandler[sn.Node.String()] {
		close(c)
	}
	delete(sn.sim.protocolHandler, sn.Node.String())
	sn.sim.mutex.Unlock()
}
