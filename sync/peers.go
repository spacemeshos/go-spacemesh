package sync

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync/atomic"
)

type Peer crypto.PublicKey

type Peers interface {
	GetPeers() []Peer
	Close()
}

type PeersImpl struct {
	snapshot atomic.Value
	exit     chan struct{}
}

func NewPeers(s service.Service) Peers {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := &PeersImpl{snapshot: value, exit: make(chan struct{})}
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

func (pi *PeersImpl) listenToPeers(newPeerC chan crypto.PublicKey, expiredPeerC chan crypto.PublicKey) {
	peerSet := make(map[Peer]bool) //set of uniq peers
	for {
		select {
		case <-pi.exit:
			log.Debug("run stoped")
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
