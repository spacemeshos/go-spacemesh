package peerinfo

import (
	"context"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
)

type Kind string

const (
	KindUknown            Kind = ""
	KindInbound           Kind = "inbound"
	KindOutbound          Kind = "outbound"
	KindHolePunchInbound  Kind = "hp_inbound"
	KindHolePunchOutbound Kind = "hp_outbound"
	KindHolePunchUnknown  Kind = "hp_unknown"
	KindRelayInbound      Kind = "relay_in"
	KindRelayOutbound     Kind = "relay_out"

	bpsInterval1 = 10 * time.Second
	bpsInterval2 = 5 * time.Minute

	totalProto = "__total__"
	otherProto = "__other__"
)

type PeerRequestStats struct {
	mtx          sync.Mutex
	successCount int
	failureCount int
	duration     time.Duration
}

func (ps *PeerRequestStats) SuccessCount() int {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return ps.successCount
}

func (ps *PeerRequestStats) FailureCount() int {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return ps.failureCount
}

func (ps *PeerRequestStats) Latency() time.Duration {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	count := ps.successCount + ps.failureCount
	if count == 0 {
		return 0
	}
	return ps.duration / time.Duration(count)
}

func (ps *PeerRequestStats) RequestDone(took time.Duration, success bool) {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	if success {
		ps.successCount++
	} else {
		ps.failureCount++
	}
	ps.duration += took
}

type DataStats struct {
	mtx sync.Mutex
	// [0] is the current value
	// [1] is prev value for rate 1
	// [2] is prev value for rate 2
	bytesSent     [3]int64
	bytesReceived [3]int64
	// [0] is for the shorter-interval rate, [1] is for the longer-interval rate
	recvRate [2]int64
	sendRate [2]int64
}

func rateIndex(which int) int {
	switch which {
	case 1, 2:
		return which - 1
	default:
		panic("bad rate index")
	}
}

func bpsInterval(which int) time.Duration {
	switch which {
	case 1:
		return bpsInterval1
	case 2:
		return bpsInterval2
	default:
		panic("bad rate index")
	}
}

func (ds *DataStats) Tick(which int) {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	interval := bpsInterval(which)
	idx := rateIndex(which)
	ds.recvRate[idx] = int64(float64(ds.bytesReceived[0]-ds.bytesReceived[which]) / interval.Seconds())
	ds.sendRate[idx] = int64(float64(ds.bytesSent[0]-ds.bytesSent[which]) / interval.Seconds())
	ds.bytesSent[which] = ds.bytesSent[0]
	ds.bytesReceived[which] = ds.bytesReceived[0]
}

func (ds *DataStats) RecvRate(which int) int64 {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	return ds.recvRate[rateIndex(which)]
}

func (ds *DataStats) SendRate(which int) int64 {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	return ds.sendRate[rateIndex(which)]
}

func (ds *DataStats) RecordSent(n int64) {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	ds.bytesSent[0] += n
}

func (ds *DataStats) RecordReceived(n int64) {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	ds.bytesReceived[0] += n
}

func (ds *DataStats) BytesSent() int64 {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	return ds.bytesSent[0]
}

func (ds *DataStats) BytesReceived() int64 {
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	return ds.bytesReceived[0]
}

type Info struct {
	DataStats
	connKinds   sync.Map
	ClientStats PeerRequestStats
	ServerStats PeerRequestStats
}

func (i *Info) Kind(c network.Conn) Kind {
	k, ok := i.connKinds.Load(c.ID())
	if !ok {
		return KindUknown
	}
	return k.(Kind)
}

func (i *Info) SetKind(c network.Conn, k Kind) {
	i.connKinds.Store(c.ID(), k)
}

//go:generate mockgen -typed -package=peerinfo -destination=./mocks/mocks.go -source=./peerinfo.go

// PeerInfo provides peer-related connection status and statistics.
type PeerInfo interface {
	// EnsurePeerInfo returns Info structure for the specific peers.
	// If there's no such structure assigned for the peer yet, it creates one. The
	// Info structure will be removed when all connections to the peer are closed.
	EnsurePeerInfo(p peer.ID) *Info
	// RecordReceived records that n bytes of data has been received from peer p
	// via protocol proto.
	RecordReceived(n int64, proto protocol.ID, p peer.ID)
	// RecordReceived records that n bytes of data has been sent to the peer p
	// via protocol proto.
	RecordSent(n int64, proto protocol.ID, p peer.ID)
	// Protocols returns the list of protocols used so far, in no particular order.
	Protocols() []protocol.ID
	// EnsureProtoStats returns DataStats structure for the specified protocol,
	// allocating one if it doesn't exist yet.
	EnsureProtoStats(proto protocol.ID) *DataStats
}

type PeerInfoTracker struct {
	mtx        sync.Mutex
	info       map[peer.ID]*Info
	protoStats map[protocol.ID]*DataStats
	clock      clockwork.Clock
	syncOnce   sync.Once
	stop       context.CancelFunc
	eg         errgroup.Group
}

type Opt func(t *PeerInfoTracker)

func withClock(clock clockwork.Clock) Opt {
	return func(t *PeerInfoTracker) {
		t.clock = clock
	}
}

var _ network.Notifiee = &PeerInfoTracker{}

func NewPeerInfoTracker(opts ...Opt) *PeerInfoTracker {
	t := &PeerInfoTracker{
		info:       make(map[peer.ID]*Info),
		protoStats: make(map[protocol.ID]*DataStats),
		clock:      clockwork.NewRealClock(),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *PeerInfoTracker) Start(p2pNet network.Network) {
	t.syncOnce.Do(func() {
		var ctx context.Context
		ctx, t.stop = context.WithCancel(context.Background())
		t1 := t.clock.NewTicker(bpsInterval1)
		t2 := t.clock.NewTicker(bpsInterval2)
		p2pNet.Notify(t)
		t.eg.Go(func() error {
			defer t1.Stop()
			defer t2.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-t1.Chan():
					t.tick(1)
				case <-t2.Chan():
					t.tick(2)
				}
			}
		})
	})
}

func (t *PeerInfoTracker) Stop() {
	if t.stop == nil {
		panic("Stop without Start")
	}
	t.stop()
	t.eg.Wait()
}

func (t *PeerInfoTracker) tick(which int) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	for _, ds := range t.protoStats {
		ds.Tick(which)
	}
	for _, i := range t.info {
		i.Tick(which)
	}
}

// Connected implements network.Notifiee.
func (t *PeerInfoTracker) Connected(_ network.Network, c network.Conn) {
	kind := KindUknown
	_, err := c.RemoteMultiaddr().ValueForProtocol(ma.P_CIRCUIT)
	isRelay := err == nil
	switch c.Stat().Direction {
	case network.DirInbound:
		if isRelay {
			kind = KindRelayInbound
		} else {
			kind = KindInbound
		}
	case network.DirOutbound:
		if isRelay {
			kind = KindRelayOutbound
		} else {
			kind = KindOutbound
		}
	}
	t.EnsurePeerInfo(c.RemotePeer()).SetKind(c, kind)
}

// Disconnected implements network.Notifiee.
func (t *PeerInfoTracker) Disconnected(n network.Network, c network.Conn) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	for _, cur := range n.ConnsToPeer(c.RemotePeer()) {
		if c.ID() != cur.ID() {
			// other connections exist
			return
		}
	}
	delete(t.info, c.RemotePeer())
}

// Listen implements network.Notifiee.
func (*PeerInfoTracker) Listen(network.Network, ma.Multiaddr) {}

// ListenClose implements network.Notifiee.
func (*PeerInfoTracker) ListenClose(network.Network, ma.Multiaddr) {}

func (t *PeerInfoTracker) EnsurePeerInfo(p peer.ID) *Info {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	info, found := t.info[p]
	if !found {
		info = &Info{}
		t.info[p] = info
	}
	return info
}

func (t *PeerInfoTracker) EnsureProtoStats(proto protocol.ID) *DataStats {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if proto == "" {
		proto = otherProto
	}
	ds, found := t.protoStats[proto]
	if !found {
		ds = &DataStats{}
		t.protoStats[proto] = ds
	}
	return ds
}

func (t *PeerInfoTracker) RecordReceived(n int64, proto protocol.ID, p peer.ID) {
	t.EnsureProtoStats(proto).RecordReceived(n)
	t.EnsureProtoStats(totalProto).RecordReceived(n)
	t.EnsurePeerInfo(p).RecordReceived(n)
}

func (t *PeerInfoTracker) RecordSent(n int64, proto protocol.ID, p peer.ID) {
	t.EnsureProtoStats(proto).RecordSent(n)
	t.EnsureProtoStats(totalProto).RecordSent(n)
	t.EnsurePeerInfo(p).RecordSent(n)
}

func (t *PeerInfoTracker) Protocols() []protocol.ID {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	return maps.Keys(t.protoStats)
}

type HolePunchTracer struct {
	pi   PeerInfo
	next holepunch.MetricsTracer
}

var _ holepunch.MetricsTracer = &HolePunchTracer{}

func NewHolePunchTracer(pi PeerInfo, next holepunch.MetricsTracer) *HolePunchTracer {
	return &HolePunchTracer{pi: pi, next: next}
}

// DirectDialFinished implements holepunch.MetricsTracer.
func (h *HolePunchTracer) DirectDialFinished(success bool) {
	h.next.DirectDialFinished(success)
}

// HolePunchFinished implements holepunch.MetricsTracer.
func (h *HolePunchTracer) HolePunchFinished(
	side string,
	attemptNum int,
	theirAddrs, ourAddr []ma.Multiaddr,
	directConn network.ConnMultiaddrs,
) {
	h.next.HolePunchFinished(side, attemptNum, theirAddrs, ourAddr, directConn)
	if h.pi == nil || directConn == nil {
		return
	}
	kind := KindHolePunchUnknown
	switch side {
	case "initiator":
		kind = KindHolePunchOutbound
	case "receiver":
		kind = KindHolePunchInbound
	}
	c := directConn.(network.Conn)
	h.pi.EnsurePeerInfo(c.RemotePeer()).SetKind(c, kind)
}
