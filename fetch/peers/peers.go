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
	failRate          float64
	averageLatency    float64
}

func (d *data) latency(global float64) float64 {
	if d.success+d.failures == 0 {
		return 0.8 * global // to prioritize trying out new peer
	}
	if d.success == 0 {
		return global + d.failRate*global
	}
	return d.averageLatency + d.failRate*global
}

func (p *data) less(other *data, global float64) bool {
	peerLatency := p.latency(global)
	otherLatency := other.latency(global)
	if peerLatency < otherLatency {
		return true
	} else if peerLatency > otherLatency {
		return false
	}
	return strings.Compare(string(p.id), string(other.id)) == -1
}

func New() *Peers {
	return &Peers{
		peers: map[peer.ID]*data{},
	}
}

type Peers struct {
	mu    sync.Mutex
	peers map[peer.ID]*data

	// globalLatency is the average latency of all successful responses from peers.
	// It is used as a reference value for new peers.
	// And to adjust average peer latency based on failure rate.
	globalLatency float64
}

func (p *Peers) Add(id peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, exist := p.peers[id]
	if exist {
		return
	}
	p.peers[id] = &data{id: id}
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
	peer.failRate = float64(peer.failures) / float64(peer.success+peer.failures)
}

// OnLatency updates average peer and global latency.
func (p *Peers) OnLatency(id peer.ID, latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	peer, exist := p.peers[id]
	if !exist {
		return
	}
	peer.success++
	peer.failRate = float64(peer.failures) / float64(peer.success+peer.failures)
	if peer.averageLatency != 0 {
		peer.averageLatency = 0.8*peer.averageLatency + 0.2*float64(latency)
	} else {
		peer.averageLatency = float64(latency)
	}
	if p.globalLatency != 0 {
		p.globalLatency = 0.8*p.globalLatency + 0.2*float64(latency)
	} else {
		p.globalLatency = float64(latency)
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
		if best == nil {
			best = pdata
		} else if pdata.less(best, p.globalLatency) {
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
	best := make([]*data, 0, lth)
	for _, peer := range p.peers {
		worst := peer
		for i := range best {
			if worst.less(best[i], p.globalLatency) {
				best[i], worst = worst, best[i]
			}
		}
		if len(best) < cap(best) {
			best = append(best, worst)
		}
	}
	rst := make([]peer.ID, len(best))
	for i := range rst {
		rst[i] = best[i].id
	}
	return rst
}

func (p *Peers) Total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.peers)
}
