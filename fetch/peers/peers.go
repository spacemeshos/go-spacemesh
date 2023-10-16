package peers

import (
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

type data struct {
	id                peer.ID
	success, failures int
	rate              float64
	averageLatency    float64
}

func (p *data) successRate() float64 {
	return float64(p.success) / float64(p.success+p.failures)
}

func (p *data) cmp(other *data, rateThreshold float64) int {
	if p == nil && other != nil {
		return -1
	}
	switch {
	case p.rate-other.rate > rateThreshold:
		return 1
	case other.rate-p.rate > rateThreshold:
		return -1
	}
	switch {
	case p.averageLatency < other.averageLatency:
		return 1
	case p.averageLatency > other.averageLatency:
		return -1
	}
	return strings.Compare(string(p.id), string(other.id))
}

type Opt func(*Peers)

func WithRateThreshold(rate float64) Opt {
	return func(p *Peers) {
		p.rateThreshold = rate
	}
}

func New(opts ...Opt) *Peers {
	p := &Peers{
		peers:         map[peer.ID]*data{},
		rateThreshold: 0.1,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type Peers struct {
	mu    sync.Mutex
	peers map[peer.ID]*data

	rateThreshold float64
}

func (p *Peers) Add(id peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, exist := p.peers[id]
	if exist {
		return
	}
	peer := &data{id: id}
	p.peers[id] = peer
}

func (p *Peers) Delete(id peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.peers, id)
}

func (p *Peers) OnFailure(id peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	peer, exist := p.peers[id]
	if !exist {
		return
	}
	peer.failures++
	peer.rate = peer.successRate()
}

// OnLatency records success and latency. Latency is not reported with every success
// as some requests has different amount of work and data, and we want to measure something
// comparable.
func (p *Peers) OnLatency(id peer.ID, latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	peer, exist := p.peers[id]
	if !exist {
		return
	}
	peer.success++
	peer.rate = peer.successRate()
	if peer.averageLatency != 0 {
		peer.averageLatency = 0.8*peer.averageLatency + 0.2*float64(latency)
	} else {
		peer.averageLatency = float64(latency)
	}
}

// SelectBest peer with preferences.
func (p *Peers) SelectBestFrom(peers []peer.ID) peer.ID {
	p.mu.Lock()
	defer p.mu.Unlock()
	var best *data
	for _, peer := range peers {
		pdata, exist := p.peers[peer]
		if !exist {
			continue
		}
		if best.cmp(pdata, p.rateThreshold) == -1 {
			best = pdata
		}
	}
	if best != nil {
		return best.id
	}
	return p2p.NoPeer
}

// SelectBest selects at most n peers sorted by responsiveness and latency.
//
// SelectBest parametrized by N because sync protocol relies on receiving data from redundant
// connections to guarantee that it will get complete data set.
// If it doesn't get complete data set it will have to fallback into hash resolution, which is
// generally more expensive.
func (p *Peers) SelectBest(n int) []peer.ID {
	p.mu.Lock()
	defer p.mu.Unlock()
	lth := min(len(p.peers), n)
	if lth == 0 {
		return nil
	}
	cache := make([]*data, 0, lth)
	for _, peer := range p.peers {
		worst := peer
		for i := range cache {
			if cache[i].cmp(worst, p.rateThreshold) == -1 {
				cache[i], worst = worst, cache[i]
			}
		}
		if len(cache) < cap(cache) {
			cache = append(cache, worst)
		}
	}
	rst := make([]peer.ID, len(cache))
	for i := range rst {
		rst[i] = cache[i].id
	}
	return rst
}

func (p *Peers) Total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.peers)
}
