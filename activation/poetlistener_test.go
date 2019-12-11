package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type ServiceMock struct {
	ch chan service.GossipMessage
}

func (ServiceMock) Start() error { panic("implement me") }

func (s *ServiceMock) RegisterGossipProtocol(protocol string) chan service.GossipMessage { return s.ch }

func (ServiceMock) SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey) {
	panic("implement me")
}

func (ServiceMock) Broadcast(protocol string, payload []byte) error { panic("implement me") }

func (ServiceMock) Shutdown() { panic("implement me") }

type mockMsg struct {
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

func (m *mockMsg) ReportValidation(protocol string) { m.validationReported = true }

type PoetDbIMock struct {
	validationErr error
}

func (p *PoetDbIMock) Validate(proof types.PoetProof, poetId []byte, roundId string, signature []byte) error {
	return p.validationErr
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
	r.True(validMsg.validationReported) // message gets propagated

	// send invalid message
	invalidMsg := mockMsg{}
	poetDb.validationErr = fmt.Errorf("bad poet message")
	svc.ch <- &invalidMsg
	time.Sleep(2 * time.Millisecond)
	r.False(invalidMsg.validationReported) // message does not get propagated

	listener.Close()
}
