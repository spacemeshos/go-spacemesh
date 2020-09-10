package peers

import (
	"github.com/spacemeshos/go-spacemesh/events"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// Peer is represented by a p2p identity public key.
type Peer p2pcrypto.PublicKey

// Peers is used by protocols to manage available peers.
type Peers struct {
	log.Log
	snapshot *atomic.Value
	exit     chan struct{}

	rand *rand.Rand
}

// PeerSubscriptionProvider is the interface that provides us with peer events channels.
type PeerSubscriptionProvider interface {
	SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey)
}

// NewPeers creates a Peers instance that is registered to `s`'s events and updates with them.
func NewPeers(s PeerSubscriptionProvider, lg log.Log) *Peers {
	value := atomic.Value{}
	value.Store(make([]Peer, 0, 20))
	pi := NewPeersImpl(&value, make(chan struct{}), lg)
	newPeerC, expiredPeerC := s.SubscribePeerEvents()
	go pi.listenToPeers(newPeerC, expiredPeerC)
	events.ReportNodeStatusUpdate()
	return pi
}

// NewPeersImpl creates a Peers using specified parameters and returns it
func NewPeersImpl(snapshot *atomic.Value, exit chan struct{}, lg log.Log) *Peers {
	return &Peers{
		snapshot: snapshot,
		Log:      lg,
		exit:     exit,
		rand:     rand.New(rand.NewSource(time.Now().UnixNano()).(rand.Source64)),
	}
}

// Close stops listening for events.
func (p *Peers) Close() {
	close(p.exit)
}

// GetPeers returns a snapshot of the connected peers shuffled.
// Method is not concurrent-safe.
func (p *Peers) GetPeers() []Peer {
	peers := p.snapshot.Load().([]Peer)
	cpy := make([]Peer, len(peers))
	copy(cpy, peers) // if we dont copy we will shuffle orig array
	p.With().Info("now connected", log.Int("n_peers", len(cpy)))
	p.rand.Shuffle(len(cpy), func(i, j int) { cpy[i], cpy[j] = cpy[j], cpy[i] }) // shuffle peers order
	return cpy
}

// PeerCount returns the number of connected peers.
func (p *Peers) PeerCount() uint64 {
	peers := p.snapshot.Load().([]Peer)
	return uint64(len(peers))
}

func (p *Peers) listenToPeers(newPeerC, expiredPeerC chan p2pcrypto.PublicKey) {
	peerSet := make(map[Peer]struct{}) // set of unique peers
	defer p.Debug("run stopped")
	for {
		select {
		case <-p.exit:
			return
		case peer, ok := <-newPeerC:
			if !ok {
				return
			}
			p.With().Debug("new peer", log.String("peer", peer.String()))
			peerSet[peer] = struct{}{}
		case peer, ok := <-expiredPeerC:
			if !ok {
				return
			}
			p.With().Debug("expired peer", log.String("peer", peer.String()))
			delete(peerSet, peer)
		}
		keys := make([]Peer, 0, len(peerSet))
		for k := range peerSet {
			keys = append(keys, k)
		}
		p.snapshot.Store(keys) //swap snapshot
		events.ReportNodeStatusUpdate()
	}
}
