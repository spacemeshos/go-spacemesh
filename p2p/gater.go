package p2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type gater struct {
	h   host.Host
	max int
}

func (*gater) InterceptPeerDial(_ peer.ID) bool {
	return true
}

func (g *gater) InterceptAddrDial(pid peer.ID, m multiaddr.Multiaddr) bool {
	return len(g.h.Network().Peers()) <= g.max
}

func (g *gater) InterceptAccept(n network.ConnMultiaddrs) bool {
	return len(g.h.Network().Peers()) <= g.max
}

func (*gater) InterceptSecured(_ network.Direction, _ peer.ID, _ network.ConnMultiaddrs) bool {
	return true
}

func (*gater) InterceptUpgraded(_ network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}
