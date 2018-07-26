package simulator

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"io"
	"sync"
)

// TODO : implmement delays?

// Simulator is a p2p node factory and message bridge
type Simulator struct {
	ingressChannel chan service.Message
	io.Closer
	mutex           sync.RWMutex
	protocolHandler map[string]map[string]chan service.Message // maps peerPubkey -> protocol -> handler
}

// Node is a simulated p2p node that can be used as a p2p service
type Node struct {
	sim *Simulator
	node.Node
}

// New Creates a p2p simulation by providing nodes as p2p services and bridge them.
func New() *Simulator {
	s := &Simulator{
		ingressChannel:  make(chan service.Message),
		protocolHandler: make(map[string]map[string]chan service.Message),
	}
	return s
}

// NewNode creates a new p2p node in this Simulator
func (s *Simulator) NewNode() *Node {
	n := node.GenerateRandomNodeData()
	sn := &Node{
		s,
		n,
	}
	s.mutex.Lock()
	s.protocolHandler[n.PublicKey().String()] = make(map[string]chan service.Message)
	s.mutex.Unlock()
	return sn
}

// NewNodeFrom creates a new node from existing details
func (s *Simulator) NewNodeFrom(n node.Node) *Node {
	sn := &Node{
		s,
		n,
	}
	s.mutex.Lock()
	s.protocolHandler[n.PublicKey().String()] = make(map[string]chan service.Message)
	s.mutex.Unlock()
	return sn
}

type simMessage struct {
	msg    []byte
	sender node.Node
}

// Data is the message's binary data in byte array format.
func (sm simMessage) Data() []byte {
	return sm.msg
}

// Sender is the node who sent this message
func (sm simMessage) Sender() node.Node {
	return sm.sender
}

// SendMessage sends a protocol message to the specified nodeID.
// returns error if the node cant be found. corresponds to `Service.SendMessage`
func (sn *Node) SendMessage(nodeID string, protocol string, payload []byte) error {
	sn.sim.mutex.RLock()
	thec, ok := sn.sim.protocolHandler[nodeID][protocol]
	sn.sim.mutex.RUnlock()
	if ok {
		thec <- simMessage{payload, sn.Node}
	}
	return nil
}

// RegisterProtocol creates and returns a channel for a given protocol.
func (sn *Node) RegisterProtocol(protocol string) chan service.Message {
	c := make(chan service.Message)
	sn.sim.mutex.Lock()
	sn.sim.protocolHandler[sn.Node.String()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
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
