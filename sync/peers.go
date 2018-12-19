package sync

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
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

func NewPeers(newPeerC chan crypto.PublicKey, expiredPeerC chan crypto.PublicKey) Peers {
	pi := &PeersImpl{atomic.Value{}, make(chan struct{})}
	pi.listenToPeers(newPeerC, expiredPeerC)
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
			close(newPeerC)
			close(expiredPeerC)
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
