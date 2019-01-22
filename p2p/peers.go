package p2p

import (
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

type Peer p2pcrypto.PublicKey

type Peers interface {
	GetPeers() []Peer
	Close()
}

type PeersImpl struct {
	snapshot *atomic.Value
	exit     chan struct{}
}

// NewPeersImpl creates a PeersImpl using specified parameters and returns it
func NewPeersImpl(snapshot *atomic.Value, exit chan struct{}) *PeersImpl {
	return &PeersImpl{snapshot: snapshot, exit: exit}
}

func NewPeers(s service.Service) Peers {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := NewPeersImpl(&value, make(chan struct{}))
	newPeerC, expiredPeerC := s.SubscribePeerEvents()
	go pi.listenToPeers(newPeerC, expiredPeerC)
	return pi
}

func (pi PeersImpl) Close() {
	close(pi.exit)
}

func (pi PeersImpl) GetPeers() []Peer {
	return pi.snapshot.Load().([]Peer)
}

func (pi *PeersImpl) listenToPeers(newPeerC chan p2pcrypto.PublicKey, expiredPeerC chan p2pcrypto.PublicKey) {
	peerSet := make(map[Peer]bool) //set of uniq peers
	for {
		select {
		case <-pi.exit:
			log.Debug("run stopped")
			return
		case peer := <-newPeerC:
			peerSet[peer] = true
		case peer := <-expiredPeerC:
			delete(peerSet, peer)
		}
		keys := make([]Peer, 0, len(peerSet))
		for k := range peerSet {
			keys = append(keys, k)
		}
		pi.snapshot.Store(keys) //swap snapshot
	}
}
