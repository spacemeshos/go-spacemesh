package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	pingProtectTag = "spacemesh-ping"
)

type pingStat struct {
	numSuccess int
	numFail    int
}

type Ping struct {
	sync.Mutex
	logger   *zap.Logger
	h        host.Host
	peers    []peer.ID
	pr       routing.PeerRouting
	interval time.Duration
	stats    map[peer.ID]*pingStat
	cancel   context.CancelFunc
	eg       errgroup.Group
}

type PingOpt func(p *Ping)

func WithPingInterval(d time.Duration) PingOpt {
	return func(p *Ping) {
		p.interval = d
	}
}

func NewPing(logger *zap.Logger, h host.Host, peers []peer.ID, pr routing.PeerRouting, opts ...PingOpt) *Ping {
	p := &Ping{
		logger:   logger,
		h:        h,
		peers:    peers,
		pr:       pr,
		stats:    make(map[peer.ID]*pingStat),
		interval: time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Ping) Start() {
	if p.cancel != nil {
		return
	}
	var startCtx context.Context
	startCtx, p.cancel = context.WithCancel(context.Background())
	for _, peer := range p.peers {
		p.startForPeer(startCtx, peer)
	}
}

func (p *Ping) Stop() {
	if p.cancel == nil {
		return
	}
	p.cancel()
	p.cancel = nil
	p.eg.Wait()
}

func (p *Ping) doPing(ctx context.Context, peerID peer.ID) (<-chan ping.Result, error) {
	_, err := p.pr.FindPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("failed to find peer %s: %w", peerID, err)
	}

	return ping.Ping(ctx, p.h, peerID), nil
}

// runForPeer runs ping for the specific peer till an error occurs
// or the context is canceled.
func (p *Ping) runForPeer(ctx context.Context, peerID peer.ID, addrs []ma.Multiaddr) error {
	// go-libp2p Ping tends to stop working after an error is received on the channel.
	// This behavior is probably not intended and may change later.
	// So, we create a child context here and cancel it when we receive an error
	// on the channel returned by Ping, and then after a delay we restart
	// Ping for this peer.
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := p.doPing(pingCtx, peerID)
	if err != nil {
		return err
	}

	for r := range ch {
		p.record(peerID, r.Error == nil)
		if r.Error != nil {
			return r.Error
		} else {
			p.logger.Info("PING succeeded",
				zap.Stringer("peer", peerID),
				zap.Any("addrs", addrs),
				zap.Duration("rtt", r.RTT))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(p.interval):
		}
	}

	return nil
}

func (p *Ping) startForPeer(ctx context.Context, peerID peer.ID) {
	p.h.ConnManager().Protect(peerID, pingProtectTag)
	p.eg.Go(func() error {
		for {
			var addrs []ma.Multiaddr
			for _, c := range p.h.Network().ConnsToPeer(peerID) {
				addrs = append(addrs, c.RemoteMultiaddr())
			}
			err := p.runForPeer(ctx, peerID, addrs)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			p.logger.Error("PING failed",
				zap.Stringer("peer", peerID),
				zap.Any("addrs", addrs),
				zap.Error(err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(p.interval):
			}
		}
	})
}

func (p *Ping) record(peerID peer.ID, success bool) {
	p.Lock()
	defer p.Unlock()
	st := p.stats[peerID]
	if st == nil {
		st = &pingStat{}
		p.stats[peerID] = st
	}
	if success {
		p.stats[peerID].numSuccess++
	} else {
		p.stats[peerID].numFail++
	}
}

func (p *Ping) Stats(peerID peer.ID) (numSuccess, numFail int) {
	p.Lock()
	defer p.Unlock()
	st, found := p.stats[peerID]
	if !found {
		return 0, 0
	}
	return st.numSuccess, st.numFail
}
