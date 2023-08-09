package p2p

import (
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var _ connmgr.ConnectionGater = (*gater)(nil)

type gater struct {
	h                 host.Host
	inbound, outbound int
	direct            map[peer.ID]struct{}
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

func (*gater) InterceptAddrDial(pid peer.ID, m multiaddr.Multiaddr) bool {
	return true
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
