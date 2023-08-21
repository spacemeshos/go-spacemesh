package p2p

import "github.com/libp2p/go-libp2p/core/peer"

// Peer is an alias to libp2p's peer.ID.
type Peer = peer.ID

// NoPeer is used when peer doesn't matter.
const NoPeer Peer = ""

// IsNoPeer checks if it's any peer.
func IsNoPeer(p Peer) bool {
	return p == NoPeer
}
