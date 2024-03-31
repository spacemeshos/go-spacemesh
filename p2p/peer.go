package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/spacemeshos/go-spacemesh/p2p/conninfo"
)

// Peer is an alias to libp2p's peer.ID.
type Peer = peer.ID

// PeerInfo groups relevant information about a peer.
type PeerInfo struct {
	ID          Peer
	Connections []ConnectionInfo
	Tags        []string
}

type PeerConnectionStats struct {
	SuccessCount  int
	FailureCount  int
	Latency       time.Duration
	BytesSent     int
	BytesReceived int
}

type ConnectionInfo struct {
	Address         ma.Multiaddr
	Uptime          time.Duration
	Outbound        bool
	Kind            conninfo.Kind
	ClientConnStats PeerConnectionStats
	ServerConnStats PeerConnectionStats
}

// NoPeer is used when peer doesn't matter.
const NoPeer Peer = ""

// IsNoPeer checks if it's any peer.
func IsNoPeer(p Peer) bool {
	return p == NoPeer
}

func grabPeerConnStats(stats *conninfo.PeerConnectionStats) PeerConnectionStats {
	return PeerConnectionStats{
		SuccessCount:  stats.SuccessCount(),
		FailureCount:  stats.FailureCount(),
		Latency:       stats.Latency(),
		BytesSent:     stats.BytesSent(),
		BytesReceived: stats.BytesReceived(),
	}
}
