package gossip

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

//go:generate mockgen -package=gossip -destination=./protocol_mock_test.go -source=./protocol.go peersManager, baseNetwork, prioQ

func TestProcessMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	net := NewMockbaseNetwork(ctrl)
	peersManager := NewMockpeersManager(ctrl)
	protocol := NewProtocol(context.TODO(), config.SwarmConfig{}, net, peersManager, nil, logtest.New(t))
	t.Cleanup(func() {
		peersManager.EXPECT().Close()
		protocol.Close()
	})

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
	protocol := NewProtocol(context.TODO(), config.SwarmConfig{}, net, peersManager, nil, logtest.New(t))
	t.Cleanup(func() {
		peersManager.EXPECT().Close()
		protocol.Close()
	})

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
	isWritten bool
	isClosed  bool
	bus       chan struct{}
	called    chan struct{}
}

func (mpq *mockPriorityQueue) Write(priorityq.Priority, interface{}) error {
	mpq.isWritten = true
	mpq.called <- struct{}{}
	mpq.bus <- struct{}{}
	return nil
}

func (mpq mockPriorityQueue) Read() (interface{}, error) {
	return <-mpq.bus, nil
}

func (mpq *mockPriorityQueue) Close() {
	mpq.isClosed = true
}

func (mpq *mockPriorityQueue) Length() int {
	return len(mpq.bus)
}

func TestPropagationEventLoop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	peersManager := NewMockpeersManager(ctrl)
	peersManager.EXPECT().Close()
	protocol := NewProtocol(context.Background(), config.SwarmConfig{}, nil, peersManager, nil, logtest.New(t))
	called := make(chan struct{})
	mpq := mockPriorityQueue{called: called, bus: make(chan struct{}, 10)}
	protocol.pq = &mpq

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		protocol.propagationEventLoop(context.TODO())
		wg.Done()
	}()

	protocol.propagateQ <- service.MessageValidation{}
	<-called
	assert.Equal(t, true, mpq.isWritten, "message should be written")
	assert.Equal(t, false, mpq.isClosed, "listener should not be shut down yet")

	mpq.isWritten = false
	protocol.Close()
	wg.Wait()
	assert.Equal(t, false, mpq.isWritten, "message should not be written")
	assert.Equal(t, true, mpq.isClosed, "listener should be shut down")

	protocol.propagateQ <- service.MessageValidation{}
	timeout := time.NewTimer(time.Second)
	select {
	case <-called:
		assert.Fail(t, "queue should not be written to after shutdown")
	case <-timeout.C:
		assert.Equal(t, false, mpq.isWritten, "message should not be written")
		assert.Equal(t, true, mpq.isClosed, "listener should be shut down")
	}
}
