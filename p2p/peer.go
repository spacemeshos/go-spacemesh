package p2p

import "github.com/libp2p/go-libp2p-core/peer"

// Peer is an alias to libp2p's peer.ID.
type Peer = peer.ID

// AnyPeer is used when peer doesn't matter.
const AnyPeer Peer = ""

// IsAnyPeer checks if it's any peer.
func IsAnyPeer(p Peer) bool {
	return p == AnyPeer
}
