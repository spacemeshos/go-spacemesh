package conninfo

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	ma "github.com/multiformats/go-multiaddr"
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
)

type Info struct {
	kind            atomic.Value
	ClientConnStats PeerConnectionStats
	ServerConnStats PeerConnectionStats
}

func (i *Info) Kind() Kind {
	k := i.kind.Load()
	if k == nil {
		return KindUknown
	}
	return k.(Kind)
}

func (i *Info) SetKind(k Kind) {
	i.kind.Store(k)
}

type ConnInfo interface {
	EnsureConnInfo(c network.Conn) *Info
}

type ConnInfoTracker struct {
	sync.Mutex
	info map[string]*Info
}

var _ network.Notifiee = &ConnInfoTracker{}

func NewConnInfoTracker(n network.Network) *ConnInfoTracker {
	t := &ConnInfoTracker{
		info: make(map[string]*Info),
	}
	n.Notify(t)
	return t
}

// Connected implements network.Notifiee.
func (t *ConnInfoTracker) Connected(_ network.Network, c network.Conn) {
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
	t.EnsureConnInfo(c).SetKind(kind)
}

// Disconnected implements network.Notifiee.
func (t *ConnInfoTracker) Disconnected(_ network.Network, c network.Conn) {
	t.Lock()
	defer t.Unlock()
	delete(t.info, c.ID())
}

// Listen implements network.Notifiee.
func (*ConnInfoTracker) Listen(network.Network, ma.Multiaddr) {}

// ListenClose implements network.Notifiee.
func (*ConnInfoTracker) ListenClose(network.Network, ma.Multiaddr) {}

func (t *ConnInfoTracker) EnsureConnInfo(c network.Conn) *Info {
	t.Lock()
	defer t.Unlock()
	id := c.ID()
	info, found := t.info[id]
	if !found {
		info = &Info{}
		t.info[id] = info
	}
	return info
}

func (t *ConnInfoTracker) WrapStreamHandler(handler network.StreamHandler) network.StreamHandler {
	return func(s network.Stream) {
		handler(NewCountingStream(t, s, true))
	}
}

func (t *ConnInfoTracker) WrapClientStream(s network.Stream) network.Stream {
	return NewCountingStream(t, s, false)
}

type HolePunchTracer struct {
	next holepunch.MetricsTracer
	ci   ConnInfo
}

var _ holepunch.MetricsTracer = &HolePunchTracer{}

func NewHolePunchTracer(next holepunch.MetricsTracer) *HolePunchTracer {
	return &HolePunchTracer{next: next}
}

func (h *HolePunchTracer) SetConnInfo(ci ConnInfo) {
	h.ci = ci
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
	if h.ci == nil || directConn == nil {
		return
	}
	kind := KindHolePunchUnknown
	switch side {
	case "initiator":
		kind = KindHolePunchOutbound
	case "receiver":
		kind = KindHolePunchInbound
	}
	h.ci.EnsureConnInfo(directConn.(network.Conn)).SetKind(kind)
}

type Host struct {
	host.Host
	*ConnInfoTracker
}

func NewHost(h host.Host) *Host {
	return &Host{Host: h, ConnInfoTracker: NewConnInfoTracker(h.Network())}
}

func (h *Host) SetStreamHandler(pid protocol.ID, handler network.StreamHandler) {
	h.Host.SetStreamHandler(pid, h.WrapStreamHandler(handler))
}

func (h *Host) SetStreamHandlerMatch(pid protocol.ID, match func(protocol.ID) bool, handler network.StreamHandler) {
	h.Host.SetStreamHandlerMatch(pid, match, h.WrapStreamHandler(handler))
}

func (h *Host) NewStream(ctx context.Context, p peer.ID, pids ...protocol.ID) (network.Stream, error) {
	s, err := h.Host.NewStream(ctx, p, pids...)
	if err != nil {
		return nil, err
	}
	return h.WrapClientStream(s), nil
}
