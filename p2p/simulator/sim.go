package simulator

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"io"
	"sync"
)

type simulator struct {
	ingressChannel chan service.Message
	io.Closer
	mutex           sync.RWMutex
	protocolHandler map[string]map[string]chan service.Message // maps peerPubkey -> protocol -> handler
}

type Node struct {
	sim *simulator
	node.Node
}

func New() *simulator {
	s := &simulator{
		ingressChannel:  make(chan service.Message),
		protocolHandler: make(map[string]map[string]chan service.Message),
	}
	return s
}

func (s *simulator) NewNode() *Node {
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

func (s *simulator) NewNodeFrom(n node.Node) *Node {
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

func (sm simMessage) Data() []byte {
	return sm.msg
}

func (sm simMessage) Sender() node.Node {
	return sm.sender
}

func (sn *Node) SendMessage(nodeID string, protocol string, payload []byte) error {
	sn.sim.mutex.RLock()
	thec, ok := sn.sim.protocolHandler[nodeID][protocol]
	sn.sim.mutex.RUnlock()
	if ok {
		thec <- simMessage{payload, sn.Node}
	}
	return nil
}

func (sn *Node) RegisterProtocol(protocol string) chan service.Message {
	c := make(chan service.Message)
	sn.sim.mutex.Lock()
	sn.sim.protocolHandler[sn.Node.String()][protocol] = c
	sn.sim.mutex.Unlock()
	return c
}
