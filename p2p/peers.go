package p2p

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
	"sync/atomic"
)

type Peer p2pcrypto.PublicKey

type Peers struct {
	log.Log
	snapshot *atomic.Value
	exit     chan struct{}
}

// NewPeersImpl creates a Peers using specified parameters and returns it
func NewPeersImpl(snapshot *atomic.Value, exit chan struct{}, lg log.Log) *Peers {
	return &Peers{snapshot: snapshot, Log: lg, exit: exit}
}

type PeerSubscriptionProvider interface {
	SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey)
}

func NewPeers(s PeerSubscriptionProvider, lg log.Log) *Peers {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := NewPeersImpl(&value, make(chan struct{}), lg)
	newPeerC, expiredPeerC := s.SubscribePeerEvents()
	go pi.listenToPeers(newPeerC, expiredPeerC)
	return pi
}

func (pi Peers) Close() {
	close(pi.exit)
}

func (pi Peers) GetPeers() []Peer {
	peers := pi.snapshot.Load().([]Peer)
	cpy := make([]Peer, len(peers))
	copy(cpy, peers) // if we dont copy we will shuffle orig array
	pi.With().Info("now connected", log.Int("n_peers", len(cpy)))
	rand.Shuffle(len(cpy), func(i, j int) { cpy[i], cpy[j] = cpy[j], cpy[i] }) // shuffle peers order
	return cpy
}

func (pi Peers) PeerCount() uint64 {
	peers := pi.snapshot.Load().([]Peer)
	return uint64(len(peers))
}

func (pi *Peers) listenToPeers(newPeerC, expiredPeerC chan p2pcrypto.PublicKey) {
	peerSet := make(map[Peer]bool) // set of unique peers
	defer pi.Debug("run stopped")
	for {
		select {
		case <-pi.exit:
			return
		case peer, ok := <-newPeerC:
			if !ok {
				return
			}
			pi.With().Debug("new peer", log.String("peer", peer.String()))
			peerSet[peer] = true
		case peer, ok := <-expiredPeerC:
			if !ok {
				return
			}
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
