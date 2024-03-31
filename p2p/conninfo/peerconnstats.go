package conninfo

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

type PeerConnectionStats struct {
	sync.Mutex
	successCount  int
	failureCount  int
	duration      time.Duration
	bytesSent     atomic.Int64
	bytesReceived atomic.Int64
}

func (ps *PeerConnectionStats) SuccessCount() int {
	ps.Lock()
	defer ps.Unlock()
	return ps.successCount
}

func (ps *PeerConnectionStats) FailureCount() int {
	ps.Lock()
	defer ps.Unlock()
	return ps.failureCount
}

func (ps *PeerConnectionStats) Latency() time.Duration {
	ps.Lock()
	defer ps.Unlock()
	count := ps.successCount + ps.failureCount
	if count == 0 {
		return 0
	}
	return ps.duration / time.Duration(count)
}

func (ps *PeerConnectionStats) RequestDone(took time.Duration, success bool) {
	ps.Lock()
	defer ps.Unlock()
	if success {
		ps.successCount++
	} else {
		ps.failureCount++
	}
	ps.duration += took
}

func (ps *PeerConnectionStats) recordSent(n int) {
	ps.bytesSent.Add(int64(n))
}

func (ps *PeerConnectionStats) recordReceived(n int) {
	ps.bytesReceived.Add(int64(n))
}

func (ps *PeerConnectionStats) BytesSent() int {
	return int(ps.bytesSent.Load())
}

func (ps *PeerConnectionStats) BytesReceived() int {
	return int(ps.bytesReceived.Load())
}

type CountingStream struct {
	network.Stream
	stats *PeerConnectionStats
}

var _ network.Stream = &CountingStream{}

func NewCountingStream(ci ConnInfo, s network.Stream, server bool) *CountingStream {
	info := ci.EnsureConnInfo(s.Conn())
	cs := &CountingStream{Stream: s}
	if server {
		cs.stats = &info.ServerConnStats
	} else {
		cs.stats = &info.ClientConnStats
	}
	return cs
}

func (cs *CountingStream) Read(p []byte) (n int, err error) {
	n, err = cs.Stream.Read(p)
	cs.stats.recordReceived(n)
	return n, err
}

func (cs *CountingStream) Write(p []byte) (n int, err error) {
	n, err = cs.Stream.Write(p)
	cs.stats.recordSent(n)
	return n, err
}
