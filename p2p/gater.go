package p2p

import (
	"fmt"
	"net"
	"sync"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var _ connmgr.ConnectionGater = (*gater)(nil)

func newGater(cfg Config) (*gater, error) {
	// leaves a small room for outbound connections in order to
	// reduce risk of network isolation
	g := &gater{
		inbound:    int(float64(cfg.HighPeers) * cfg.InboundFraction),
		outbound:   int(float64(cfg.HighPeers) * cfg.OutboundFraction),
		direct:     map[peer.ID]struct{}{},
		iplimit:    cfg.IPLimit,
		ipsCounter: map[string]int{},
	}
	direct, err := parseIntoAddr(cfg.Direct)
	if err != nil {
		return nil, err
	}
	if !cfg.PrivateNetwork {
		g.ip4blocklist, err = parseCIDR(cfg.IP4Blocklist)
		if err != nil {
			return nil, err
		}
		g.ip6blocklist, err = parseCIDR(cfg.IP6Blocklist)
		if err != nil {
			return nil, err
		}
	}
	for _, pid := range direct {
		g.direct[pid.ID] = struct{}{}
	}
	return g, nil
}

type gater struct {
	h                 host.Host
	inbound, outbound int
	direct            map[peer.ID]struct{}
	ip4blocklist      []*net.IPNet
	ip6blocklist      []*net.IPNet

	iplimit    int
	mu         sync.Mutex
	ipsCounter map[string]int
}

func (g *gater) updateHost(h host.Host) {
	g.h = h
	notifiee := &network.NotifyBundle{
		DisconnectedF: func(_ network.Network, c network.Conn) {
			g.OnDisconnected(c.RemoteMultiaddr())
		},
		ConnectedF: func(_ network.Network, c network.Conn) {
			g.OnConnected(c.RemoteMultiaddr())
		},
	}
	g.h.Network().Notify(notifiee)
}

func (g *gater) OnDisconnected(remote multiaddr.Multiaddr) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ip := parseIP(remote); ip != nil {
		n, exists := g.ipsCounter[string(ip)]
		if !exists {
			return
		}
		if n == 1 {
			delete(g.ipsCounter, string(ip))
		} else {
			g.ipsCounter[string(ip)]--
		}
	}
}

func (g *gater) OnConnected(remote multiaddr.Multiaddr) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ip := parseIP(remote); ip != nil {
		g.ipsCounter[string(ip)]++
	}
}

func (g *gater) InterceptPeerDial(pid peer.ID) bool {
	if _, exist := g.direct[pid]; exist {
		return true
	}
	return len(g.h.Network().Peers()) <= g.outbound
}

func (g *gater) InterceptAddrDial(pid peer.ID, m multiaddr.Multiaddr) bool {
	if _, exist := g.direct[pid]; exist {
		return true
	}
	return len(g.h.Network().Peers()) <= g.outbound && g.allowed(m)
}

func (g *gater) InterceptAccept(n network.ConnMultiaddrs) bool {
	if len(g.h.Network().Peers()) > g.inbound {
		return false
	}
	if ip := parseIP(n.RemoteMultiaddr()); ip != nil {
		g.mu.Lock()
		n := g.ipsCounter[string(ip)]
		g.mu.Unlock()
		return g.iplimit > n+1
	}
	return true
}

func (*gater) InterceptSecured(_ network.Direction, _ peer.ID, _ network.ConnMultiaddrs) bool {
	return true
}

func (*gater) InterceptUpgraded(_ network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}

func (g *gater) allowed(m multiaddr.Multiaddr) bool {
	allow := true
	multiaddr.ForEach(m, func(c multiaddr.Component) bool {
		switch c.Protocol().Code {
		case multiaddr.P_IP4:
			allow = !inAddrRange(net.IP(c.RawValue()), g.ip4blocklist)
			return false
		case multiaddr.P_IP6:
			allow = !inAddrRange(net.IP(c.RawValue()), g.ip6blocklist)
			return false
		}
		return true
	})
	return allow
}

func parseIP(m multiaddr.Multiaddr) (ip net.IP) {
	multiaddr.ForEach(m, func(c multiaddr.Component) bool {
		switch c.Protocol().Code {
		case multiaddr.P_IP4:
			ip = c.RawValue()
			return false
		case multiaddr.P_IP6:
			ip = c.RawValue()
			return false
		}
		return true
	})
	return ip
}

func parseCIDR(cidrs []string) ([]*net.IPNet, error) {
	ipnets := make([]*net.IPNet, len(cidrs))
	for i, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("can't parse %s as valid cidr: %w", cidr, err)
		}
		ipnets[i] = ipnet
	}
	return ipnets, nil
}

func inAddrRange(ip net.IP, ipnets []*net.IPNet) bool {
	for _, ipnet := range ipnets {
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}
