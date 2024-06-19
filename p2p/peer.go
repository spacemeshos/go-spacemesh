package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
)

// Peer is an alias to libp2p's peer.ID.
type Peer = peer.ID

// PeerInfo groups relevant information about a peer.
type PeerInfo struct {
	ID          Peer
	Connections []ConnectionInfo
	ClientStats PeerRequestStats
	ServerStats PeerRequestStats
	DataStats   DataStats
	Tags        []string
}

type DataStats struct {
	BytesSent     int64
	BytesReceived int64
	SendRate      [2]int64
	RecvRate      [2]int64
}

type PeerRequestStats struct {
	SuccessCount int
	FailureCount int
	Latency      time.Duration
}

type ConnectionInfo struct {
	Address  ma.Multiaddr
	Uptime   time.Duration
	Outbound bool
	Kind     peerinfo.Kind
}

// NoPeer is used when peer doesn't matter.
const NoPeer Peer = ""

// IsNoPeer checks if it's any peer.
func IsNoPeer(p Peer) bool {
	return p == NoPeer
}

func grabPeerConnStats(stats *peerinfo.PeerRequestStats) PeerRequestStats {
	return PeerRequestStats{
		SuccessCount: stats.SuccessCount(),
		FailureCount: stats.FailureCount(),
		Latency:      stats.Latency(),
	}
}
