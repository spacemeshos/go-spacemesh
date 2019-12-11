package p2p

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

func getPeers(p Service) (Peers, chan p2pcrypto.PublicKey, chan p2pcrypto.PublicKey) {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := &PeersImpl{snapshot: &value, exit: make(chan struct{}), Log: log.NewDefault("peers")}
	n, expired := p.SubscribePeerEvents()
	go pi.listenToPeers(n, expired)
	return pi, n, expired
}

func TestPeers_GetPeers(t *testing.T) {
	pi, n, _ := getPeers(service.NewSimulator().NewNode())
	a := p2pcrypto.NewRandomPubkey()
	n <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	defer pi.Close()
	assert.True(t, len(peers) == 1, "number of peers incorrect")
	assert.True(t, peers[0] == a, "returned wrong peer")
}

func TestPeers_Close(t *testing.T) {
	pi, n, _ := getPeers(service.NewSimulator().NewNode())
	a := p2pcrypto.NewRandomPubkey()
	n <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	pi.Close()
	//_, ok := <-new
	//assert.True(t, !ok, "channel 'new' still open")
	//_, ok = <-expierd
	//assert.True(t, !ok, "channel 'expierd' still open")
}

func TestPeers_AddPeer(t *testing.T) {
	pi, n, _ := getPeers(service.NewSimulator().NewNode())
	a := p2pcrypto.NewRandomPubkey()
	b := p2pcrypto.NewRandomPubkey()
	c := p2pcrypto.NewRandomPubkey()
	d := p2pcrypto.NewRandomPubkey()
	e := p2pcrypto.NewRandomPubkey()
	n <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	assert.True(t, len(peers) == 1, "number of peers incorrect, length was ", len(peers))
	n <- b
	n <- c
	n <- d
	n <- e
	defer pi.Close()
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers = pi.GetPeers()
	assert.True(t, len(peers) == 5, "number of peers incorrect, length was ", len(peers))
}

func TestPeers_RemovePeer(t *testing.T) {
	pi, n, expierd := getPeers(service.NewSimulator().NewNode())
	a := p2pcrypto.NewRandomPubkey()
	b := p2pcrypto.NewRandomPubkey()
	c := p2pcrypto.NewRandomPubkey()
	d := p2pcrypto.NewRandomPubkey()
	e := p2pcrypto.NewRandomPubkey()
	n <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	assert.True(t, len(peers) == 1, "number of peers incorrect, length was ", len(peers))
	n <- b
	n <- c
	n <- d
	n <- e
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

func TestPeers_RandomPeers(t *testing.T) {
	pi, n, _ := getPeers(service.NewSimulator().NewNode())
	a := p2pcrypto.NewRandomPubkey()
	b := p2pcrypto.NewRandomPubkey()
	c := p2pcrypto.NewRandomPubkey()
	d := p2pcrypto.NewRandomPubkey()
	e := p2pcrypto.NewRandomPubkey()
	n <- a
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers := pi.GetPeers()
	assert.True(t, len(peers) == 1, "number of peers incorrect, length was ", len(peers))
	n <- b
	n <- c
	n <- d
	n <- e
	defer pi.Close()
	time.Sleep(10 * time.Millisecond) //allow context switch
	peers1 := pi.GetPeers()
	peers2 := pi.GetPeers()
	assert.True(t, len(peers1) == 5, "number of peers incorrect, length was ", len(peers1))

	for p := range peers1 {
		if bytes.Compare(peers1[p].Bytes(), peers2[p].Bytes()) != 0 {
			t.Log("test done ")
			return
		}
		t.Log(fmt.Sprintf("index %d same element %s %s", p, peers1[p].String(), peers2[p].String()))
	}

	t.Fail()
}
