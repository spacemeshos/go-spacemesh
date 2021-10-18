package activation

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

type ServiceMock struct {
	ch chan service.GossipMessage
}

func (ServiceMock) Start(ctx context.Context) error { panic("implement me") }

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

func (ServiceMock) Broadcast(ctx context.Context, protocol string, payload []byte) error {
	panic("implement me")
}

func (ServiceMock) Shutdown() { panic("implement me") }

type mockMsg struct {
	lock               sync.Mutex
	validationReported bool
}

func (m *mockMsg) Sender() p2pcrypto.PublicKey { panic("implement me") }

func (m *mockMsg) IsOwnMessage() bool { panic("not implemented") }

func (m *mockMsg) Bytes() []byte {
	b, err := types.InterfaceToBytes(&types.PoetProofMessage{})
	if err != nil {
		panic(err)
	}
	return b
}

func (m *mockMsg) RequestID() string {
	return "fake_request_id"
}

func (m *mockMsg) ValidationCompletedChan() chan service.MessageValidation { panic("implement me") }

func (m *mockMsg) ReportValidation(ctx context.Context, protocol string) {
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
	lg := logtest.New(t)
	poetDb := PoetDbIMock{}
	listener := NewPoetListener(&poetDb, lg)

	msg, err := types.InterfaceToBytes(&types.PoetProofMessage{})
	require.NoError(t, err)
	require.Equal(t, pubsub.ValidationAccept,
		listener.HandlePoetProofMessage(context.TODO(), lp2p.Peer("test"), msg))

	poetDb.SetErr(fmt.Errorf("bad poet message"))
	require.Equal(t, pubsub.ValidationIgnore,
		listener.HandlePoetProofMessage(context.TODO(), lp2p.Peer("test"), msg))
}
