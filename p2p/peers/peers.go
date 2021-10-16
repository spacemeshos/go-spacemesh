package peers

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// Peer is represented by a p2p identity public key.
type Peer p2pcrypto.PublicKey

// PeerSubscriptionProvider is the interface that provides us with peer events channels.
type PeerSubscriptionProvider interface {
	SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey)
}

type waitPeersReq struct {
	ch  chan []Peer
	min int
}

// Option to modify peers instance.
type Option func(*Peers)

// WithLog sets logger for Peers instance.
func WithLog(lg log.Log) Option {
	return func(p *Peers) {
		p.log = lg
	}
}

// WithNodeStatesReporter sets callback to report node status.
// Callback is invoked after new peer joins or disconnects.
func WithNodeStatesReporter(f func()) Option {
	return func(p *Peers) {
		p.nodeReporter = f
	}
}

// Start creates a Peers instance that is registered to `s`'s events and starts it.
func Start(s PeerSubscriptionProvider, opts ...Option) *Peers {
	p := New(opts...)
	added, expired := s.SubscribePeerEvents()
	p.Start(added, expired)
	return p
}

// New creates a Peers using specified parameters and returns it.
func New(opts ...Option) *Peers {
	p := &Peers{
		log:      log.NewNop(),
		exit:     make(chan struct{}),
		requests: make(chan *waitPeersReq),
	}
	p.snapshot.Store(make([]Peer, 0, 20))
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Peers is used by protocols to manage available peers.
type Peers struct {
	log      log.Log
	snapshot atomic.Value
	requests chan *waitPeersReq

	wg   sync.WaitGroup
	exit chan struct{}

	nodeReporter func()
}

// Close stops listening for events and waits for background worker to exit.
func (p *Peers) Close() {
	select {
	case <-p.exit:
		return
	default:
		close(p.exit)
		p.wg.Wait()
	}
}

// GetPeers returns a snapshot of the connected peers shuffled.
func (p *Peers) GetPeers() []Peer {
	peers := p.snapshot.Load().([]Peer)
	return peers
}

// PeerCount returns the number of connected peers.
func (p *Peers) PeerCount() uint64 {
	peers := p.snapshot.Load().([]Peer)
	return uint64(len(peers))
}

// WaitPeers returns with atleast N peers or when context is terminated.
// Nil slice is returned if Peers is closing.
func (p *Peers) WaitPeers(ctx context.Context, n int) ([]Peer, error) {
	if n == 0 {
		return nil, nil
	}

	req := waitPeersReq{min: n, ch: make(chan []Peer, 1)}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context done: %w", ctx.Err())
	case <-p.exit:
		return nil, nil
	case p.requests <- &req:
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context done: %w", ctx.Err())
	case <-p.exit:
		return nil, nil
	case prs := <-req.ch:
		return prs, nil
	}
}

// Start listener goroutine in background.
func (p *Peers) Start(added, expired chan p2pcrypto.PublicKey) {
	p.wg.Add(1)
	go func() {
		p.listen(added, expired)
		p.wg.Done()
	}()
}

func (p *Peers) listen(added, expired chan p2pcrypto.PublicKey) {
	var (
		peerSet  = make(map[Peer]struct{})
		requests = map[*waitPeersReq]struct{}{}
		isAdded  bool
	)
	defer p.log.Debug("peers events listener is stopped")
	for {
		select {
		case <-p.exit:
			return
		case peer, ok := <-added:
			if !ok {
				return
			}
			isAdded = true
			peerSet[peer] = struct{}{}
			p.log.With().Debug("new peer",
				log.String("peer", peer.String()),
				log.Int("total", len(peerSet)),
			)
		case peer, ok := <-expired:
			if !ok {
				return
			}
			isAdded = false
			delete(peerSet, peer)
			p.log.With().Debug("expired peer",
				log.String("peer", peer.String()),
				log.Int("total", len(peerSet)),
			)
		case req := <-p.requests:
			if len(peerSet) >= req.min {
				req.ch <- p.snapshot.Load().([]Peer)
			} else {
				requests[req] = struct{}{}
			}
		}
		keys := make([]Peer, 0, len(peerSet))
		for k := range peerSet {
			keys = append(keys, k)
		}
		if isAdded {
			for req := range requests {
				if len(peerSet) >= req.min {
					req.ch <- keys
					delete(requests, req)
				}
			}
		}
		p.snapshot.Store(keys)
		if p.nodeReporter != nil {
			p.nodeReporter()
		}
	}
}
