package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
)

// ConnectionsStat is a snapshot of connections statistics.
type ConnectionsStat struct {
	TotalConnections   int32
	StreamsPerProtocol map[protocol.ID]int
}

// ConnectionsMeeter stores the number of connections for node.
// number of connections
// number of streams per each protocol
// histogram for server request durations for each protocol
type ConnectionsMeeter struct {
	connections        int32
	durationHistogram  *prometheus.HistogramVec
	streamsPerProtocol struct {
		sync.RWMutex
		m map[protocol.ID]int
	}
}

// NewConnectionsMeeter returns a new ConnectionsMeeter.
func NewConnectionsMeeter() *ConnectionsMeeter {
	connMeeter := &ConnectionsMeeter{
		streamsPerProtocol: struct {
			sync.RWMutex
			m map[protocol.ID]int
		}{
			m: make(map[protocol.ID]int),
		},
		durationHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "p2p_requests_duration",
				Help: "Histogram of server request durations per protocol",
			},
			[]string{"protocol"},
		),
	}
	_ = prometheus.Register(connMeeter.durationHistogram)
	return connMeeter
}

// GetStat returns the current ConnectionsStat.
func (c *ConnectionsMeeter) GetStat() *ConnectionsStat {
	c.streamsPerProtocol.RLock()
	defer c.streamsPerProtocol.RUnlock()
	return &ConnectionsStat{
		TotalConnections:   atomic.LoadInt32(&c.connections),
		StreamsPerProtocol: c.streamsPerProtocol.m,
	}
}

// Listen called when network starts listening on an addr.
func (c *ConnectionsMeeter) Listen(network.Network, ma.Multiaddr) {}

// ListenClose called when network stops listening on an addr.
func (c *ConnectionsMeeter) ListenClose(network.Network, ma.Multiaddr) {}

// Connected called when a connection opened.
func (c *ConnectionsMeeter) Connected(network.Network, network.Conn) {
	atomic.AddInt32(&c.connections, 1)
}

// Disconnected called when a connection closed.
func (c *ConnectionsMeeter) Disconnected(network.Network, network.Conn) {
	atomic.AddInt32(&c.connections, -1)
}

// OpenedStream called when a stream opened.
func (c *ConnectionsMeeter) OpenedStream(_ network.Network, str network.Stream) {
	c.streamsPerProtocol.Lock()
	c.streamsPerProtocol.m[str.Protocol()]++
	c.streamsPerProtocol.Unlock()
}

// ClosedStream called when a stream closed.
func (c *ConnectionsMeeter) ClosedStream(_ network.Network, str network.Stream) {
	protocolID := str.Protocol()
	c.streamsPerProtocol.Lock()
	if c.streamsPerProtocol.m[protocolID] > 0 {
		c.streamsPerProtocol.m[protocolID]--
	}
	c.streamsPerProtocol.Unlock()

	// log stream duration
	duration := time.Since(str.Stat().Opened)
	c.durationHistogram.WithLabelValues(string(str.Protocol())).Observe(duration.Seconds())
}
