package p2p

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
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
	stats  map[peer.ID]*pingStat
	ctx    context.Context
	cancel context.CancelFunc
	eg     errgroup.Group
}

func NewPing(logger log.Log, h host.Host, peers []peer.ID) *Ping {
	p := &Ping{
		logger: logger,
		h:      h,
		peers:  peers,
		stats:  make(map[peer.ID]*pingStat),
	}
	return p
}

func (p *Ping) Start() {
	if p.cancel != nil {
		return
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.logger.With().Info("QQQQQ: start ping (common)", log.Any("peers", p.peers))
	for _, peer := range p.peers {
		p.startForPeer(peer)
	}
}

func (p *Ping) startForPeer(peer peer.ID) {
	p.logger.With().Info("QQQQQ: start ping", log.Stringer("peer", peer))
	ch := ping.Ping(p.ctx, p.h, peer)
	p.eg.Go(func() error {
		for r := range ch {
			var addrs []ma.Multiaddr
			for _, c := range p.h.Network().ConnsToPeer(peer) {
				addrs = append(addrs, c.RemoteMultiaddr())
			}
			p.record(peer, r.Error == nil)
			if r.Error != nil {
				p.logger.With().Error("PING failed",
					log.Stringer("peer", peer),
					log.Any("addrs", addrs),
					log.Err(r.Error))
			} else {
				p.logger.With().Info("PING succeeded",
					log.Stringer("peer", peer),
					log.Any("addrs", addrs),
					log.Duration("rtt", r.RTT))
			}
			select {
			case <-p.ctx.Done():
				return p.ctx.Err()
			case <-time.After(pingInterval):
			}
		}
		return nil
	})
}

func (p *Ping) record(peer peer.ID, success bool) {
	p.Lock()
	defer p.Unlock()
	st := p.stats[peer]
	if st == nil {
		st = &pingStat{}
		p.stats[peer] = st
	}
	if success {
		p.stats[peer].numSuccess++
	} else {
		p.stats[peer].numFail++
	}
}

func (p *Ping) Stats(peer peer.ID) (numSuccess, numFail int) {
	p.Lock()
	defer p.Unlock()
	st := p.stats[peer]
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
