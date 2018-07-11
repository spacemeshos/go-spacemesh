package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
)

var mainChan = make(chan service.Message)
var keyToChan = make(map[string]chan service.Message)
var keyToChanMutex sync.RWMutex

var initRouting sync.Once

func MsgRouting() {
	go func() {
		for {
			mc := <-mainChan
			keyToChanMutex.RLock()
			nodechan, ok := keyToChan[mc.(*msgMock).nodeID]
			keyToChanMutex.RUnlock()
			if ok {
				nodechan <- mc
			}
		}
	}()
}

type msgMock struct {
	data   []byte
	sender node.Node
	nodeID string
}

func (mm *msgMock) Data() []byte {
	return mm.data
}

func (mm *msgMock) Sender() node.Node {
	return mm.sender
}
func (mm *msgMock) Target() string {
	return mm.nodeID
}

type p2pMock struct {
	local      node.Node
	inchannel  chan service.Message
	outchannel chan service.Message
}

func newP2PMock(local node.Node) *p2pMock {
	p2p := &p2pMock{
		local:      local,
		inchannel:  make(chan service.Message),
		outchannel: mainChan,
	}
	keyToChanMutex.Lock()
	keyToChan[local.String()] = p2p.inchannel
	keyToChanMutex.Unlock()
	return p2p
}

func (p2p *p2pMock) RegisterProtocol(protocol string) chan service.Message {
	return p2p.inchannel
}

func (p2p *p2pMock) SendMessage(nodeID string, protocol string, payload []byte) error {
	p2p.outchannel <- &msgMock{payload, p2p.local, nodeID}
	return nil
}
