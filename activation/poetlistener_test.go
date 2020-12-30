package activation

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/stretchr/testify/require"
)

type ServiceMock struct {
	ch chan service.GossipMessage
}

func (ServiceMock) Start() error { panic("implement me") }

func (s *ServiceMock) RegisterGossipProtocol(protocol string, priority priorityq.Priority) chan service.GossipMessage {
	return s.ch
}
func (s *ServiceMock) RegisterDirectProtocol(protocol string) chan service.DirectMessage {
	panic("not implemented")
}

func (ServiceMock) SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey) {
	panic("implement me")
}

func (ServiceMock) GossipReady() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func (ServiceMock) Broadcast(protocol string, payload []byte) error { panic("implement me") }

func (ServiceMock) Shutdown() { panic("implement me") }

type mockMsg struct {
	lock               sync.Mutex
	validationReported bool
}

func (m *mockMsg) Sender() p2pcrypto.PublicKey { panic("implement me") }

func (m *mockMsg) Bytes() []byte {
	b, err := types.InterfaceToBytes(&types.PoetProofMessage{})
	if err != nil {
		panic(err)
	}
	return b
}

func (m *mockMsg) ValidationCompletedChan() chan service.MessageValidation { panic("implement me") }

func (m *mockMsg) ReportValidation(protocol string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.validationReported = true
}

func (m *mockMsg) GetReportValidation() bool {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.validationReported
}

type PoetDbIMock struct {
	lock          sync.Mutex
	validationErr error
}

func (p *PoetDbIMock) Validate(proof types.PoetProof, poetID []byte, roundID string, signature []byte) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.validationErr
}

func (p *PoetDbIMock) SetErr(err error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.validationErr = err
}

func (p *PoetDbIMock) storeProof(proofMessage *types.PoetProofMessage) error { return nil }

func TestNewPoetListener(t *testing.T) {
	r := require.New(t)

	lg := log.NewDefault("poet")
	svc := &ServiceMock{}
	svc.ch = make(chan service.GossipMessage)
	poetDb := PoetDbIMock{}
	listener := NewPoetListener(svc, &poetDb, lg)
	listener.Start()

	// ⚠️ IMPORTANT: We must not ensure that the node is synced! PoET messages must be propagated regardless.

	// send valid message
	validMsg := mockMsg{}
	svc.ch <- &validMsg
	time.Sleep(2 * time.Millisecond)
	r.True(validMsg.GetReportValidation()) // message gets propagated

	// send invalid message
	invalidMsg := mockMsg{}
	poetDb.SetErr(fmt.Errorf("bad poet message"))
	svc.ch <- &invalidMsg
	time.Sleep(2 * time.Millisecond)
	r.False(invalidMsg.GetReportValidation()) // message does not get propagated

	listener.Close()
}
