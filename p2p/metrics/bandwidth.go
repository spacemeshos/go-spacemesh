package metrics

import (
	"sync"

	"github.com/libp2p/go-libp2p-core/metrics"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
)

const (
	incoming = "incoming"
	outgoing = "outgoing"
)

type bandwidthStat struct {
	TotalIn             int64
	TotalOut            int64
	MessagesPerProtocol map[protocol.ID]map[string]int64
	TrafficPerProtocol  map[protocol.ID]map[string]int64
}

// BandwidthCollector wrapper over metrics.Reporter
// that keeps track of the number of messages sent and received per protocol.
type BandwidthCollector struct {
	metrics.Reporter
	messagesPerProtocol struct {
		sync.RWMutex
		m map[protocol.ID]map[string]int64
	}
}

func NewBandwidthCollector() *BandwidthCollector {
	return &BandwidthCollector{
		metrics.NewBandwidthCounter(),
		struct {
			sync.RWMutex
			m map[protocol.ID]map[string]int64
		}{
			m: make(map[protocol.ID]map[string]int64),
		},
	}
}

func (b *BandwidthCollector) GetStat() *bandwidthStat {
	stat := b.GetBandwidthTotals()
	bbb := b.GetBandwidthByProtocol()

	trafficPerProtocol := make(map[protocol.ID]map[string]int64)
	for proto, stats := range bbb {
		if _, ok := trafficPerProtocol[proto]; !ok {
			trafficPerProtocol[proto] = make(map[string]int64)
		}
		trafficPerProtocol[proto][incoming] += stats.TotalIn
		trafficPerProtocol[proto][outgoing] += stats.TotalOut
	}
	b.messagesPerProtocol.RLock()
	defer b.messagesPerProtocol.RUnlock()
	return &bandwidthStat{
		TotalIn:             stat.TotalIn,
		TotalOut:            stat.TotalOut,
		TrafficPerProtocol:  trafficPerProtocol,
		MessagesPerProtocol: b.messagesPerProtocol.m,
	}
}

func (b *BandwidthCollector) LogSentMessageStream(size int64, proto protocol.ID, p peer.ID) {
	b.messagesPerProtocol.Lock()
	if _, ok := b.messagesPerProtocol.m[proto]; !ok {
		b.messagesPerProtocol.m[proto] = make(map[string]int64)
	}
	b.messagesPerProtocol.m[proto][outgoing]++
	b.messagesPerProtocol.Unlock()
	b.Reporter.LogSentMessageStream(size, proto, p)
}

func (b *BandwidthCollector) LogRecvMessageStream(size int64, proto protocol.ID, p peer.ID) {
	b.messagesPerProtocol.Lock()
	if _, ok := b.messagesPerProtocol.m[proto]; !ok {
		b.messagesPerProtocol.m[proto] = make(map[string]int64)
	}
	b.messagesPerProtocol.m[proto][incoming]++
	b.messagesPerProtocol.Unlock()
	b.Reporter.LogRecvMessageStream(size, proto, p)
}
