package p2p

import "github.com/libp2p/go-libp2p-core/peer"

// Peer is an alias to libp2p's peer.ID.
type Peer = peer.ID

func AnyPeer() Peer {
	return ""
}

func IsAnyPeer(p Peer) bool {
	return p == ""
}
