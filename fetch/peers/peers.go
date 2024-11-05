package peers

import (
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

type data struct {
	id                peer.ID
	success, failures int
	failRate          float64
	averageLatency    float64
}

func (d *data) latency(global float64) float64 {
	switch {
	case d.success+d.failures == 0:
		return 0.9 * global // to prioritize trying out new peer
	default:
		return d.averageLatency + d.failRate*global
	}
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

func (p *Peers) Contains(id peer.ID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, exist := p.peers[id]
	return exist
}

func (p *Peers) Add(id peer.ID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, exist := p.peers[id]
	if exist {
		return false
	}
	p.peers[id] = &data{id: id}
	return true
}

func (p *Peers) Delete(id peer.ID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.peers, id)
}

// OnLatency updates average peer and global latency.
func (p *Peers) onLatency(id peer.ID, size int, latency time.Duration, failed bool) {
	// We assume that latency is proportional to the size of the message
	// and define it as a duration to transmit 1kiB.
	// To account for the additional overhead of transmitting small messages,
	// we treat them as if they were 1kiB.
	latency = latency / time.Duration(max(size/1024, 1))
	p.mu.Lock()
	defer p.mu.Unlock()
	peer, exist := p.peers[id]
	if !exist {
		return
	}
	if failed {
		peer.failures++
	} else {
		peer.success++
	}
	peer.failRate = float64(peer.failures) / float64(peer.success+peer.failures)
	if peer.averageLatency != 0 {
		delta := (float64(latency) - float64(peer.averageLatency)) / 10 // 86% of the value is the last 19
		peer.averageLatency += delta
	} else {
		peer.averageLatency = float64(latency)
	}
	if p.globalLatency != 0 {
		delta := (float64(latency) - float64(p.globalLatency)) / 25 // 86% of the value is the last 49
		p.globalLatency += delta
	} else {
		p.globalLatency = float64(latency)
	}
}

func (p *Peers) OnFailure(id peer.ID, size int, latency time.Duration) {
	p.onLatency(id, size, latency, true)
}

// OnLatency updates average peer and global latency.
func (p *Peers) OnLatency(id peer.ID, size int, latency time.Duration) {
	p.onLatency(id, size, latency, false)
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
		for i := range best {
			if peer.less(best[i], p.globalLatency) {
				best[i], peer = peer, best[i]
			}
		}
		if len(best) < cap(best) {
			best = append(best, peer)
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

func (p *Peers) Stats() Stats {
	best := p.SelectBest(5)
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := Stats{
		Total:                len(p.peers),
		GlobalAverageLatency: time.Duration(p.globalLatency),
	}
	for _, peer := range best {
		peerData, exist := p.peers[peer]
		if !exist {
			continue
		}
		stats.BestPeers = append(stats.BestPeers, PeerStats{
			ID:       peerData.id,
			Success:  peerData.success,
			Failures: peerData.failures,
			Latency:  time.Duration(peerData.averageLatency),
		})
	}
	return stats
}

type Stats struct {
	Total                int
	GlobalAverageLatency time.Duration
	BestPeers            []PeerStats
}

func (s *Stats) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddInt("total", s.Total)
	enc.AddDuration("global average latency", s.GlobalAverageLatency)
	enc.AddArray("best peers", zapcore.ArrayMarshalerFunc(func(arrEnc zapcore.ArrayEncoder) error {
		for _, peer := range s.BestPeers {
			arrEnc.AppendObject(&peer)
		}
		return nil
	}))
	return nil
}

type PeerStats struct {
	ID       peer.ID
	Success  int
	Failures int
	Latency  time.Duration
}

func (p *PeerStats) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("id", p.ID.String())
	enc.AddInt("success", p.Success)
	enc.AddInt("failures", p.Failures)
	enc.AddDuration("latency per 1024 bytes", p.Latency)
	return nil
}
