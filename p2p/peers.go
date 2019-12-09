package p2p

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"math/rand"
	"sync/atomic"
)

type Peer p2pcrypto.PublicKey

type Peers interface {
	GetPeers() []Peer
	Close()
}

type PeersImpl struct {
	log.Log
	snapshot *atomic.Value
	exit     chan struct{}
}

// NewPeersImpl creates a PeersImpl using specified parameters and returns it
func NewPeersImpl(snapshot *atomic.Value, exit chan struct{}, lg log.Log) *PeersImpl {
	return &PeersImpl{snapshot: snapshot, Log: lg, exit: exit}
}

func NewPeers(s service.Service, lg log.Log) Peers {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := NewPeersImpl(&value, make(chan struct{}), lg)
	newPeerC, expiredPeerC := s.SubscribePeerEvents()
	go pi.listenToPeers(newPeerC, expiredPeerC)
	return pi
}

func (pi PeersImpl) Close() {
	close(pi.exit)
}

func (pi PeersImpl) GetPeers() []Peer {
	peers := pi.snapshot.Load().([]Peer)
	cpy := make([]Peer, len(peers))
	copy(cpy, peers) //if we dont copy we will shuffle orig array
	pi.With().Info("now connected", log.Int("n_peers", len(cpy)))
	Shuffle(cpy) //shuffle peers order
	return cpy
}

func Shuffle(vals []Peer) {
	// We start at the end of the slice, inserting our random
	// values one at a time.
	for n := len(vals); len(vals) > 1 && n > 0; n-- {
		randIndex := rand.Intn(len(vals))
		// We swap the value at index n-1 and the random index
		// to move our randomly chosen value to the end of the
		// slice, and to move the value that was at n-1 into our
		// unshuffled portion of the slice.
		vals[n-1], vals[randIndex] = vals[randIndex], vals[n-1]
	}
}

func (pi *PeersImpl) listenToPeers(newPeerC chan p2pcrypto.PublicKey, expiredPeerC chan p2pcrypto.PublicKey) {
	peerSet := make(map[Peer]bool) //set of uniq peers
	for {
		select {
		case <-pi.exit:
			pi.Debug("run stopped")
			return
		case peer := <-newPeerC:
			pi.With().Debug("new peer", log.String("peer", peer.String()))
			peerSet[peer] = true
		case peer := <-expiredPeerC:
			pi.With().Debug("expired peer", log.String("peer", peer.String()))
			delete(peerSet, peer)
		}
		keys := make([]Peer, 0, len(peerSet))
		for k := range peerSet {
			keys = append(keys, k)
		}
		pi.snapshot.Store(keys) //swap snapshot
	}
}
