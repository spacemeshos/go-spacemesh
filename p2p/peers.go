package p2p

import (
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

type Peer crypto.PublicKey

type Peers interface {
	GetPeers() []Peer
	Close()
}

type PeersImpl struct {
	Snapshot *atomic.Value
	Exit     chan struct{}
}

func NewPeers(s service.Service) Peers {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := &PeersImpl{Snapshot: &value, Exit: make(chan struct{})}
	newPeerC, expiredPeerC := s.SubscribePeerEvents()
	go pi.listenToPeers(newPeerC, expiredPeerC)
	return pi
}

func (pi PeersImpl) Close() {
	close(pi.Exit)
}

func (pi PeersImpl) GetPeers() []Peer {
	return pi.Snapshot.Load().([]Peer)
}

func (pi *PeersImpl) listenToPeers(newPeerC chan crypto.PublicKey, expiredPeerC chan crypto.PublicKey) {
	peerSet := make(map[Peer]bool) //set of uniq peers
	for {
		select {
		case <-pi.Exit:
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
		pi.Snapshot.Store(keys) //swap snapshot
	}
}
