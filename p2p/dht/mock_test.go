package dht

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"sync"
)

var mainChan = make(chan Message)
var keyToChan = make(map[string]chan Message)
var keyToChanMutex sync.RWMutex

var initRouting sync.Once

func MsgRouting() {
	go func() {
		for {
			mc := <-mainChan
			keyToChanMutex.RLock()
			nodechan, ok := keyToChan[mc.(*msgMock).target.String()]
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
	target crypto.PublicKey
}

func (mm *msgMock) Data() []byte {
	return mm.data
}

func (mm *msgMock) Sender() node.Node {
	return mm.sender
}
func (mm *msgMock) Target() crypto.PublicKey {
	return mm.target
}

type p2pMock struct {
	local      node.Node
	inchannel  chan Message
	outchannel chan Message
}

func newP2PMock(local node.Node) *p2pMock {
	p2p := &p2pMock{
		local:      local,
		inchannel:  make(chan Message, 3),
		outchannel: mainChan,
	}
	keyToChanMutex.Lock()
	keyToChan[local.String()] = p2p.inchannel
	keyToChanMutex.Unlock()
	return p2p
}

func (p2p *p2pMock) RegisterProtocol(protocol string) chan Message {
	return p2p.inchannel
}

func (p2p *p2pMock) SendMessage(target crypto.PublicKey, msg []byte) {
	p2p.outchannel <- &msgMock{msg, p2p.local, target}
}
