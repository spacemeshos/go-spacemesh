package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

type Service service.Service

func New(config config.Config) (*swarm, error) {
	return newSwarm(config, true)
}

type Mock struct {
	sm   error
	proc chan service.Message
}

func (m *Mock) RegisterProtocol(protocol string) chan service.Message {
	return nil
}

func (m *Mock) SendMessage(nodeID, protocol string, payload []byte) error {
	return nil
}

func (m *Mock) SetSendMessage(e error) {
	m.sm = e
}

func (m *Mock) SetRegisterProtocol(csm chan service.Message) {
	m.proc = csm
}
