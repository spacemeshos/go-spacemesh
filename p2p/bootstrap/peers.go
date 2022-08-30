package bootstrap

import (
	"context"
	"sync/atomic"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/sync/errgroup"

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

// WithNodeReporter sets callback to report node status.
// Callback is invoked after new peer joins or disconnects.
func WithNodeReporter(f func()) Opt {
	return func(p *Peers) {
		p.nodeReporter = f
	}
}

// WithContext sets parent context on Peers.
func WithContext(ctx context.Context) Opt {
	return func(p *Peers) {
		p.ctx = ctx
	}
}

// StartPeers creates a Peers instance that is registered to `s`'s events and starts it.
func StartPeers(h host.Host, opts ...Opt) *Peers {
	p := newPeers(h, opts...)
	p.Start()
	return p
}

//go:generate mockgen -package=mocks -destination=./mocks/peers.go -source=./peers.go

// Waiter is an interface to wait for peers.
type Waiter interface {
	WaitPeers(context.Context, int) ([]peer.ID, error)
}

// newPeers creates a Peers using specified parameters and returns it.
func newPeers(h host.Host, opts ...Opt) *Peers {
	p := &Peers{
		h:        h,
		logger:   log.NewNop(),
		ctx:      context.Background(),
		requests: make(chan *waitPeersReq),
	}
	p.snapshot.Store(make([]peer.ID, 0, 20))
	for _, opt := range opts {
		opt(p)
	}
	p.ctx, p.cancel = context.WithCancel(p.ctx)
	return p
}

// Peers is used by protocols to manage available peers.
type Peers struct {
	logger   log.Log
	h        host.Host
	snapshot atomic.Value
	requests chan *waitPeersReq

	eg     errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc

	nodeReporter func()
}

// Stop stops listening for events and waits for background worker to exit.
func (p *Peers) Stop() {
	p.cancel()
	p.eg.Wait()
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
	case <-p.ctx.Done():
		return nil, nil
	case p.requests <- &req:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, nil
	case prs := <-req.ch:
		return prs, nil
	}
}

// Start listener goroutine in background.
func (p *Peers) Start() {
	sub, err := p.h.EventBus().Subscribe(new(EventSpacemeshPeer))
	if err != nil {
		p.logger.With().Panic("failed to subscribe for events", log.Err(err))
	}
	p.eg.Go(func() error {
		p.listen(sub)
		return p.ctx.Err()
	})
}

func (p *Peers) listen(sub event.Subscription) {
	var (
		peerSet  = make(map[peer.ID]struct{})
		requests = map[*waitPeersReq]struct{}{}
	)

	defer p.logger.Debug("peers events listener is stopped")
	for {
		before := len(peerSet)
		select {
		case <-p.ctx.Done():
			return
		case evt := <-sub.Out():
			sp, ok := evt.(EventSpacemeshPeer)
			if !ok {
				panic("expecting EventSpacemeshPeer")
			}
			pid := sp.PID
			isAdded := sp.Connectedness == network.Connected
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
		if before < len(peerSet) {
			for req := range requests {
				if len(peerSet) >= req.min {
					req.ch <- keys
					delete(requests, req)
				}
			}
		}
		if before != len(peerSet) {
			p.snapshot.Store(keys)
			if p.nodeReporter != nil {
				p.nodeReporter()
			}
		}
	}
}
