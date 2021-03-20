package gossip

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	protocol := NewProtocol(config.SwarmConfig{}, net, nil, nil, logger)

	isSent := false
	net.EXPECT().
		ProcessGossipProtocolMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Do(func(...interface{}) { isSent = true })

	err := protocol.processMessage(context.TODO(), p2pcrypto.NewRandomPubkey(), "test", service.DataBytes{Payload: []byte("test")})
	assert.NoError(t, err, "err should be nil")
	assert.Equal(t, true, isSent, "message should be sent")

	isSent = false
	err = protocol.processMessage(context.TODO(), p2pcrypto.NewRandomPubkey(), "test", service.DataBytes{Payload: []byte("test")})
	assert.NoError(t, err, "err  should be nil")
	assert.Equal(t, false, isSent, "message shouldn't be sent, cause it's already done previously")
}

func TestPropagateMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	net := NewMockbaseNetwork(ctrl)
	peersManager := NewMockpeersManager(ctrl)
	protocol := NewProtocol(config.SwarmConfig{}, net, peersManager, nil, logger)

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
		SendMessage(gomock.Any(), gomock.Any(), "test", []byte("test")).
		Do(func(peer p2pcrypto.PublicKey, _ ...interface{}) {
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

func TestPropagationEventLoop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	protocol := NewProtocol(config.SwarmConfig{}, nil, nil, nil, logger)
	pq := NewMockprioQ(ctrl)
	protocol.pq = pq

	var isWritten, isClosed bool
	pq.EXPECT().
		Write(gomock.Any(), gomock.Any()).
		Do(func(_ interface{}, _ interface{}) { isWritten = true }).
		AnyTimes()

	pq.EXPECT().
		Close().
		Do(func() { isClosed = true })

	pq.EXPECT().
		Read().
		Do(func() { time.Sleep(time.Minute * 5) }).
		AnyTimes()

	go protocol.propagationEventLoop(context.TODO())

	protocol.propagateQ <- service.MessageValidation{}
	time.Sleep(time.Second * 2)
	assert.Equal(t, true, isWritten, "message should be written")
	assert.Equal(t, false, isClosed, "listener should not be shut down yet")

	isWritten = false
	protocol.shutdown <- struct{}{}
	time.Sleep(time.Second * 2)
	assert.Equal(t, false, isWritten, "message should not be written")
	assert.Equal(t, true, isClosed, "listener should be shut down")

	protocol.propagateQ <- service.MessageValidation{}
	time.Sleep(time.Second * 2)
	assert.Equal(t, false, isWritten, "message should not be written")
	assert.Equal(t, true, isClosed, "listener should be shut down")

}
