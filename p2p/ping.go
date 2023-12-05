package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	pingInterval = time.Second
)

type pingStat struct {
	numSuccess int
	numFail    int
}

type Ping struct {
	sync.Mutex
	logger log.Log
	h      host.Host
	peers  []peer.ID
	pr     routing.PeerRouting
	stats  map[peer.ID]*pingStat
	ctx    context.Context
	cancel context.CancelFunc
	eg     errgroup.Group
}

func NewPing(logger log.Log, h host.Host, peers []peer.ID, pr routing.PeerRouting) *Ping {
	p := &Ping{
		logger: logger,
		h:      h,
		peers:  peers,
		pr:     pr,
		stats:  make(map[peer.ID]*pingStat),
	}
	return p
}

func (p *Ping) Start() {
	if p.cancel != nil {
		return
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	for _, peer := range p.peers {
		p.startForPeer(peer)
	}
}

func (p *Ping) doPing(ctx context.Context, peerID peer.ID) (<-chan ping.Result, error) {
	_, err := p.pr.FindPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("failed to find peer %s: %w", peerID, err)
	}

	return ping.Ping(ctx, p.h, peerID), nil
}

func (p *Ping) startForPeer(peerID peer.ID) {
	p.h.ConnManager().Protect(peerID, "sm-ping")

	p.eg.Go(func() error {
		var ctx context.Context
		var cancel context.CancelFunc
		defer func() {
			if cancel != nil {
				cancel()
			}
		}()
	OUTER:
		for {
			if cancel != nil {
				cancel()
			}
			ctx, cancel = context.WithCancel(p.ctx)
			ch, err := p.doPing(ctx, peerID)
			if err != nil {
				cancel()
				p.logger.With().Error("pre-PING connect failed", log.Err(err))
				select {
				case <-p.ctx.Done():
					return p.ctx.Err()
				case <-time.After(pingInterval):
					continue
				}
			}

			for r := range ch {
				var addrs []ma.Multiaddr
				for _, c := range p.h.Network().ConnsToPeer(peerID) {
					addrs = append(addrs, c.RemoteMultiaddr())
				}
				p.record(peerID, r.Error == nil)
				if r.Error != nil {
					p.logger.With().Error("PING failed",
						log.Stringer("peer", peerID),
						log.Any("addrs", addrs),
						log.Err(r.Error))
					continue OUTER
				} else {
					p.logger.With().Info("PING succeeded",
						log.Stringer("peer", peerID),
						log.Any("addrs", addrs),
						log.Duration("rtt", r.RTT))
				}
				select {
				case <-p.ctx.Done():
					cancel()
					return p.ctx.Err()
				case <-time.After(pingInterval):
				}
			}

			cancel()
			return nil
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
	st := p.stats[peerID]
	return st.numSuccess, st.numFail
}

func (p *Ping) Stop() {
	if p.cancel == nil {
		return
	}
	p.cancel()
	p.cancel = nil
	p.eg.Wait()
}
