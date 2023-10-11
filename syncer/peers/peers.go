package peers

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type data struct {
	id                peer.ID
	success, failures int
	averageLatency    float64
}

func (p *data) successRate() float64 {
	if p.success+p.failures == 0 {
		return 0
	}
	return float64(p.success) / float64(p.success+p.failures)
}

func (p *data) cmp(other *data) int {
	if p == nil && other != nil {
		return -1
	}
	const rateThreshold = 0.1
	switch {
	case p.successRate()-other.successRate() > rateThreshold:
		return 1
	case other.successRate()-p.successRate() > rateThreshold:
		return -1
	}
	switch {
	case p.averageLatency < other.averageLatency:
		return 1
	case p.averageLatency > other.averageLatency:
		return -1
	}
	return 0
}

func New() *Peers {
	return &Peers{peers: map[peer.ID]*data{}}
}

type Peers struct {
	mu    sync.Mutex
	peers map[peer.ID]*data
	best  *data
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
}

func (p *Peers) OnSuccess(id peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	peer, exist := p.peers[id]
	if !exist {
		return
	}
	peer.success++
	if p.best.cmp(peer) == -1 {
		p.best = peer
	}
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
	if peer.averageLatency != 0 {
		peer.averageLatency = 0.8*float64(peer.averageLatency) + 0.2*float64(latency)
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
		if best.cmp(pdata) == -1 {
			best = pdata
		}
	}
	if best != nil {
		return best.id
	}
	return ""

}

// SelectNBest selects at most n peers sorted by responsiveness and latency.
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
			if cache[i].cmp(worst) == -1 {
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
