package metrics

import (
	"sync"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

type GossipStat struct {
	TotalPeers       int                 // total number of peers
	PeersPerProtocol map[protocol.ID]int // number of peers per each gossip protocol
}

// GossipCollector pubsub.RawTracer implementation
// total number of peers
// number of peers per each gossip protocol
type GossipCollector struct {
	peersPerProtocol struct {
		sync.RWMutex
		m map[protocol.ID]map[peer.ID]struct{}
	}
}

// NewGoSIPCollector creates a new GossipCollector
func NewGoSIPCollector() *GossipCollector {
	return &GossipCollector{
		peersPerProtocol: struct {
			sync.RWMutex
			m map[protocol.ID]map[peer.ID]struct{}
		}{
			m: make(map[protocol.ID]map[peer.ID]struct{}),
		},
	}
}

// GetStat returns the current stat
func (g *GossipCollector) GetStat() *GossipStat {
	g.peersPerProtocol.RLock()
	peersMap := g.peersPerProtocol.m
	g.peersPerProtocol.RUnlock()

	peersPerProtocol := make(map[protocol.ID]int)
	totalPeersMap := make(map[string]struct{})
	for i := range peersMap {
		peersPerProtocol[i] = len(peersMap[i])
		for j := range peersMap[i] {
			totalPeersMap[string(j)] = struct{}{}
		}
	}

	return &GossipStat{
		TotalPeers:       len(totalPeersMap),
		PeersPerProtocol: peersPerProtocol,
	}
}

// AddPeer is invoked when a new peer is added.
func (g *GossipCollector) AddPeer(id peer.ID, proto protocol.ID) {
	g.peersPerProtocol.Lock()
	if _, ok := g.peersPerProtocol.m[proto]; !ok {
		g.peersPerProtocol.m[proto] = make(map[peer.ID]struct{})
	}
	g.peersPerProtocol.m[proto][id] = struct{}{}
	g.peersPerProtocol.Unlock()
}

// RemovePeer is invoked when a peer is removed.
func (g *GossipCollector) RemovePeer(p peer.ID) {
	g.peersPerProtocol.Lock()
	for i := range g.peersPerProtocol.m {
		if _, ok := g.peersPerProtocol.m[i][p]; ok {
			delete(g.peersPerProtocol.m[i], p)
		}
	}
	g.peersPerProtocol.Unlock()
}

// Join is invoked when a new topic is joined
func (g *GossipCollector) Join(string) {}

// Leave is invoked when a topic is abandoned
func (g *GossipCollector) Leave(string) {}

// Graft is invoked when a new peer is grafted on the mesh (gossipsub)
func (g *GossipCollector) Graft(peer.ID, string) {}

// Prune is invoked when a peer is pruned from the message (gossipsub)
func (g *GossipCollector) Prune(peer.ID, string) {}

// ValidateMessage is invoked when a message first enters the validation pipeline.
func (g *GossipCollector) ValidateMessage(*pubsub.Message) {}

// DeliverMessage is invoked when a message is delivered
func (g *GossipCollector) DeliverMessage(*pubsub.Message) {}

// RejectMessage is invoked when a message is Rejected or Ignored.
// The reason argument can be one of the named strings Reject*.
func (g *GossipCollector) RejectMessage(*pubsub.Message, string) {}

// DuplicateMessage is invoked when a duplicate message is dropped.
func (g *GossipCollector) DuplicateMessage(*pubsub.Message) {}

// ThrottlePeer is invoked when a peer is throttled by the peer gater.
func (g *GossipCollector) ThrottlePeer(peer.ID) {}

// RecvRPC is invoked when an incoming RPC is received.
func (g *GossipCollector) RecvRPC(*pubsub.RPC) {}

// SendRPC is invoked when a RPC is sent.
func (g *GossipCollector) SendRPC(*pubsub.RPC, peer.ID) {}

// DropRPC is invoked when an outbound RPC is dropped, typically because of a queue full.
func (g *GossipCollector) DropRPC(*pubsub.RPC, peer.ID) {}

// UndeliverableMessage is invoked when the consumer of Subscribe is not reading messages fast enough and
// the pressure release mechanism trigger, dropping messages.
func (g *GossipCollector) UndeliverableMessage(*pubsub.Message) {}
