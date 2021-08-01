package gossip

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

//go:generate mockgen -package=gossip -destination=./protocol_mock_test.go -source=./protocol.go peersManager, baseNetwork, prioQ

var logger = log.NewDefault("gossip-protocol-test")

func TestProcessMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	net := NewMockbaseNetwork(ctrl)
	protocol := NewProtocol(context.TODO(), config.SwarmConfig{}, net, nil, nil, logger)

	isSent := false
	net.EXPECT().
		ProcessGossipProtocolMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Do(func(...interface{}) { isSent = true })

	err := protocol.processMessage(context.TODO(), p2pcrypto.NewRandomPubkey(), false, "test", service.DataBytes{Payload: []byte("test")})
	assert.NoError(t, err, "err should be nil")
	assert.Equal(t, true, isSent, "message should be sent")

	isSent = false
	err = protocol.processMessage(context.TODO(), p2pcrypto.NewRandomPubkey(), false, "test", service.DataBytes{Payload: []byte("test")})
	assert.NoError(t, err, "err should be nil")
	assert.Equal(t, false, isSent, "message shouldn't be sent, cause it's already done previously")
}

func TestPropagateMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	net := NewMockbaseNetwork(ctrl)
	peersManager := NewMockpeersManager(ctrl)
	protocol := NewProtocol(context.TODO(), config.SwarmConfig{}, net, peersManager, nil, logger)

	peers := make([]p2ppeers.Peer, 30)
	for i := range peers {
		peers[i] = p2pcrypto.NewRandomPubkey()
	}
	exclude := peers[0]
	peersManager.EXPECT().
		GetPeers().
		Return(peers)

	peersMu := sync.Mutex{}
	handledPeers := make(map[p2ppeers.Peer]bool)
	net.EXPECT().
		SendMessage(context.TODO(), gomock.Any(), "test", []byte("test")).
		Do(func(ctx context.Context, peer p2pcrypto.PublicKey, _ ...interface{}) {
			peersMu.Lock()
			handledPeers[peer] = true
			peersMu.Unlock()
		}).
		AnyTimes()

	protocol.propagateMessage(context.TODO(), []byte("test"), "test", exclude)

	assert.Equal(t, false, handledPeers[exclude], "peer should be excluded")
	for i := 1; i < len(peers); i++ {
		assert.Equal(t, true, handledPeers[peers[i]], "peer should be handled")
	}
}

type mockPriorityQueue struct {
	mu        sync.RWMutex
	isWritten bool
	isClosed  bool
	bus       chan struct{}
	called    chan struct{}
}

func (mpq *mockPriorityQueue) Write(priorityq.Priority, interface{}) error {
	mpq.setIsWritten(true)
	called := mpq.getCalled()
	bus := mpq.getBus()

	called <- struct{}{}
	bus <- struct{}{}

	return nil
}

func (mpq *mockPriorityQueue) getIsWritten() bool {
	mpq.mu.RLock()
	defer mpq.mu.RUnlock()

	return mpq.isWritten
}

func (mpq *mockPriorityQueue) setIsWritten(value bool) {
	mpq.mu.Lock()
	defer mpq.mu.Unlock()

	mpq.isWritten = value
}

func (mpq *mockPriorityQueue) getIsClosed() bool {
	mpq.mu.RLock()
	defer mpq.mu.RUnlock()

	return mpq.isClosed
}

func (mpq *mockPriorityQueue) setIsClosed(value bool) {
	mpq.mu.Lock()
	defer mpq.mu.Unlock()

	mpq.isClosed = value
}

func (mpq *mockPriorityQueue) getCalled() chan struct{} {
	mpq.mu.RLock()
	defer mpq.mu.RUnlock()

	return mpq.called
}

func (mpq *mockPriorityQueue) setCalled(value chan struct{}) {
	mpq.mu.Lock()
	defer mpq.mu.Unlock()

	mpq.called = value
}

func (mpq *mockPriorityQueue) getBus() chan struct{} {
	mpq.mu.RLock()
	defer mpq.mu.RUnlock()

	return mpq.bus
}

func (mpq *mockPriorityQueue) setBus(value chan struct{}) {
	mpq.mu.Lock()
	defer mpq.mu.Unlock()

	mpq.bus = value
}

func (mpq *mockPriorityQueue) Read() (interface{}, error) {
	bus := mpq.getBus()
	return <-bus, nil
}

func (mpq *mockPriorityQueue) Close() {
	mpq.setIsClosed(true)
	called := mpq.getCalled()
	called <- struct{}{}
}

func (mpq *mockPriorityQueue) Length() int {
	return len(mpq.getBus())
}

func TestPropagationEventLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	protocol := NewProtocol(ctx, config.SwarmConfig{}, nil, nil, nil, log.AppLog)
	called := make(chan struct{})
	mpq := mockPriorityQueue{called: called, bus: make(chan struct{}, 10)}
	protocol.pq = &mpq

	go protocol.propagationEventLoop(context.TODO())

	protocol.propagateQ <- service.MessageValidation{}
	<-called
	assert.Equal(t, true, mpq.getIsWritten(), "message should be written")
	assert.Equal(t, false, mpq.getIsClosed(), "listener should not be shut down yet")

	mpq.setIsWritten(false)
	cancel()
	<-called
	assert.Equal(t, false, mpq.getIsWritten(), "message should not be written")
	assert.Equal(t, true, mpq.getIsClosed(), "listener should be shut down")

	protocol.propagateQ <- service.MessageValidation{}
	timeout := time.NewTimer(time.Second)
	select {
	case <-called:
		assert.Fail(t, "queue should not be written to after shutdown")
	case <-timeout.C:
		assert.Equal(t, false, mpq.getIsWritten(), "message should not be written")
		assert.Equal(t, true, mpq.getIsClosed(), "listener should be shut down")
	}
}
