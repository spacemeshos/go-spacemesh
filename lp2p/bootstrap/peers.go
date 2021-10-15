package bootstrap

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/log"
)

type waitPeersReq struct {
	ch  chan []peer.ID
	min int
}

// Opt to modify peers instance.
type Opt func(*Peers)

// WithLog sets logger for Peers instance.
func WithLog(logger log.Log) Opt {
	return func(p *Peers) {
		p.logger = logger
	}
}

// WithNodeStatesReporter sets callback to report node status.
// Callback is invoked after new peer joins or disconnects.
func WithNodeStatesReporter(f func()) Opt {
	return func(p *Peers) {
		p.nodeReporter = f
	}
}

// StartPeers creates a Peers instance that is registered to `s`'s events and starts it.
func StartPeers(h host.Host, opts ...Opt) *Peers {
	p := NewPeers(h, opts...)
	p.Start()
	return p
}

// NewPeers creates a Peers using specified parameters and returns it.
func NewPeers(h host.Host, opts ...Opt) *Peers {
	p := &Peers{
		h:        h,
		logger:   log.NewNop(),
		exit:     make(chan struct{}),
		requests: make(chan *waitPeersReq),
	}
	p.snapshot.Store(make([]peer.ID, 0, 20))
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Peers is used by protocols to manage available peers.
type Peers struct {
	logger   log.Log
	h        host.Host
	snapshot atomic.Value
	requests chan *waitPeersReq

	wg   sync.WaitGroup
	exit chan struct{}

	nodeReporter func()
}

// Stop stops listening for events and waits for background worker to exit.
func (p *Peers) Stop() {
	select {
	case <-p.exit:
		return
	default:
		close(p.exit)
		p.wg.Wait()
	}
}

// GetPeers returns a snapshot of the connected peers shuffled.
func (p *Peers) GetPeers() []peer.ID {
	peers, _ := p.snapshot.Load().([]peer.ID)
	return peers
}

// PeerCount returns the number of connected peers.
func (p *Peers) PeerCount() uint64 {
	peers, _ := p.snapshot.Load().([]peer.ID)
	return uint64(len(peers))
}

// WaitPeers returns with atleast N peers or when context is terminated.
// Nil slice is returned if Peers is closing.
func (p *Peers) WaitPeers(ctx context.Context, n int) ([]peer.ID, error) {
	req := waitPeersReq{min: n, ch: make(chan []peer.ID, 1)}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.exit:
		return nil, nil
	case p.requests <- &req:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.exit:
		return nil, nil
	case prs := <-req.ch:
		return prs, nil
	}
}

// Start listener goroutine in background.
func (p *Peers) Start() {
	p.wg.Add(1)
	sub, err := p.h.EventBus().Subscribe(new(EventSpacemeshPeer))
	if err != nil {
		p.logger.With().Panic("failed to subscribe for events", log.Err(err))
	}
	go func() {
		p.listen(sub)
		p.wg.Done()
	}()
}

func (p *Peers) listen(sub event.Subscription) {
	var (
		peerSet  = make(map[peer.ID]struct{})
		requests = map[*waitPeersReq]struct{}{}
		isAdded  bool
	)

	defer p.logger.Debug("peers events listener is stopped")
	for {
		select {
		case <-p.exit:
			return
		case evt := <-sub.Out():
			sp, ok := evt.(EventSpacemeshPeer)
			if !ok {
				panic("expecting EventSpacemeshPeer")
			}
			pid := sp.PID
			isAdded = sp.Connectedness == network.Connected
			if isAdded {
				peerSet[pid] = struct{}{}
				p.logger.With().Debug("new peer",
					log.String("peer", pid.String()),
					log.Int("total", len(peerSet)),
				)
			} else {
				delete(peerSet, pid)
				p.logger.With().Debug("expired peer",
					log.String("peer", pid.String()),
					log.Int("total", len(peerSet)),
				)
			}
		case req := <-p.requests:
			if len(peerSet) >= req.min {
				req.ch <- p.snapshot.Load().([]peer.ID)
			} else {
				requests[req] = struct{}{}
			}
		}
		keys := make([]peer.ID, 0, len(peerSet))
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
