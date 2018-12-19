package sync

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

func getPeers(p server.ServerService) (Peers, chan crypto.PublicKey, chan crypto.PublicKey) {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := &PeersImpl{snapshot: value, exit: make(chan struct{})}
	new := make(chan crypto.PublicKey)
	expierd := make(chan crypto.PublicKey)
	go pi.listenToPeers(new, expierd)
	return pi, new, expierd
}

func TestPeers_GetPeers(t *testing.T) {
	pi, new, _ := getPeers(service.NewSimulator().NewNode())
	_, a, _ := crypto.GenerateKeyPair()
	new <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	defer pi.Close()
	assert.True(t, len(peers) == 1, "number of peers incorrect")
	assert.True(t, peers[0] == a, "returned wrong peer")
}

func TestPeers_Close(t *testing.T) {
	pi, new, expierd := getPeers(service.NewSimulator().NewNode())
	_, a, _ := crypto.GenerateKeyPair()
	new <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	pi.Close()
	_, ok := <-new
	assert.True(t, !ok, "channel 'new' still open")
	_, ok = <-expierd
	assert.True(t, !ok, "channel 'expierd' still open")
}

func TestPeers_AddPeer(t *testing.T) {
	pi, new, _ := getPeers(service.NewSimulator().NewNode())
	_, a, _ := crypto.GenerateKeyPair()
	_, b, _ := crypto.GenerateKeyPair()
	_, c, _ := crypto.GenerateKeyPair()
	_, d, _ := crypto.GenerateKeyPair()
	_, e, _ := crypto.GenerateKeyPair()
	new <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	assert.True(t, len(peers) == 1, "number of peers incorrect, length was ", len(peers))
	new <- b
	new <- c
	new <- d
	new <- e
	defer pi.Close()
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers = pi.GetPeers()
	assert.True(t, len(peers) == 5, "number of peers incorrect, length was ", len(peers))
}

func TestPeers_RemovePeer(t *testing.T) {
	pi, new, expierd := getPeers(service.NewSimulator().NewNode())
	_, a, _ := crypto.GenerateKeyPair()
	_, b, _ := crypto.GenerateKeyPair()
	_, c, _ := crypto.GenerateKeyPair()
	_, d, _ := crypto.GenerateKeyPair()
	_, e, _ := crypto.GenerateKeyPair()
	new <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	assert.True(t, len(peers) == 1, "number of peers incorrect, length was ", len(peers))
	new <- b
	new <- c
	new <- d
	new <- e
	defer pi.Close()
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers = pi.GetPeers()
	assert.True(t, len(peers) == 5, "number of peers incorrect, length was ", len(peers))
	expierd <- b
	expierd <- c
	expierd <- d
	expierd <- e
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers = pi.GetPeers()
	assert.True(t, len(peers) == 1, "number of peers incorrect, length was ", len(peers))
}
