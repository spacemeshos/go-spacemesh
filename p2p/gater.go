package p2p

import (
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var _ connmgr.ConnectionGater = (*gater)(nil)

func newGater(cfg Config) (*gater, error) {
	// leaves a small room for outbound connections in order to
	// reduce risk of network isolation
	g := &gater{
		inbound:  int(float64(cfg.HighPeers) * cfg.InboundFraction),
		outbound: int(float64(cfg.HighPeers) * cfg.OutboundFraction),
		direct:   map[peer.ID]struct{}{},
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
}

func (g *gater) updateHost(h host.Host) {
	g.h = h
}

func (g *gater) InterceptPeerDial(pid peer.ID) bool {
	if _, exist := g.direct[pid]; exist {
		return true
	}
	return len(g.h.Network().Peers()) <= g.outbound
}

func (g *gater) InterceptAddrDial(pid peer.ID, m ma.Multiaddr) bool {
	if _, exist := g.direct[pid]; exist {
		return true
	}
	return len(g.h.Network().Peers()) <= g.outbound && g.allowed(m)
}

func (g *gater) InterceptAccept(n network.ConnMultiaddrs) bool {
	return len(g.h.Network().Peers()) <= g.inbound
}

func (*gater) InterceptSecured(_ network.Direction, _ peer.ID, _ network.ConnMultiaddrs) bool {
	return true
}

func (*gater) InterceptUpgraded(_ network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}

func (g *gater) allowed(m ma.Multiaddr) bool {
	allow := true
	ma.ForEach(m, func(c ma.Component) bool {
		switch c.Protocol().Code {
		case ma.P_IP4:
			allow = !inAddrRange(net.IP(c.RawValue()), g.ip4blocklist)
			return false
		case ma.P_IP6:
			allow = !inAddrRange(net.IP(c.RawValue()), g.ip6blocklist)
			return false
		}
		return true
	})
	return allow
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
