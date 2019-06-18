// Package discovery implements a Distributed Hash Table based on Kademlia protocol.
package discovery

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

// PeerStore is an interface to the discovery protocol
type PeerStore interface {
	Remove(pubkey p2pcrypto.PublicKey)
	Lookup(pubkey p2pcrypto.PublicKey) (*node.NodeInfo, error)
	Update(addr, src *node.NodeInfo)
	SelectPeers(qty int) []*node.NodeInfo
	Bootstrap(ctx context.Context) error
	Size() int
	SetLocalAddresses(tcp, udp int)
}

type Protocol interface {
	Ping(p p2pcrypto.PublicKey) error
	GetAddresses(server p2pcrypto.PublicKey) ([]*node.NodeInfo, error)
	SetLocalAddresses(tcp, udp int)
}

var (
	// ErrLookupFailed determines that we could'nt find this node in the routing table or network
	ErrLookupFailed = errors.New("failed to find node in the network")
	// ErrEmptyRoutingTable means that our routing table is empty thus we can't find any node (so we can't query any)
	ErrEmptyRoutingTable = errors.New("no nodes to query - routing table is empty")
)

// Discovery is struct that holds the protocol components, the protocol definition, the addr book data structure and more.
type Discovery struct {
	config config.SwarmConfig

	disc Protocol

	local        *node.LocalNode
	rt           *addrBook
	bootstrapper *refresher
}

// Size returns the size of addrBook.
func (d *Discovery) Size() int {
	return d.rt.NumAddresses()
}

// SelectPeers asks routing table to randomly select a slice of nodes in size `qty`
func (d *Discovery) SelectPeers(qty int) []*node.NodeInfo {
	out := make([]*node.NodeInfo, 0, qty)
	set := make(map[p2pcrypto.PublicKey]struct{})
	for i := 0; i < qty; i++ {
		add := d.rt.GetAddress()
		if add == nil {
			// addrbook is empty
			return out
		}

		if _, ok := set[add.na.PublicKey()]; ok {
			continue
		}
		out = append(out, add.DiscNode())
		set[add.DiscNode().PublicKey()] = struct{}{}
	}
	return out
}

// Lookup searched a node in the address book. *NOTE* this returns a `Node` with the udpAddress as `Address()`.
// this is because Lookup is only used in the udp mux.
func (d *Discovery) Lookup(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
	return d.rt.Lookup(key)
}

// Update adds an addr to the addrBook
func (d *Discovery) Update(addr, src *node.NodeInfo) {
	d.rt.AddAddress(addr, src)
}

// TODO: Replace `node.LocalNode` with `NodeInfo` and `log.Log`.
// New creates a new Discovery
func New(ln *node.LocalNode, config config.SwarmConfig, service server.Service) *Discovery {
	d := &Discovery{
		config: config,
		local:  ln,
		rt:     NewAddrBook(ln.NodeInfo, config, ln.Log),
	}

	d.disc = NewDiscoveryProtocol(ln.NodeInfo, d.rt, service, ln.Log)

	bn := make([]*node.NodeInfo, 0, len(config.BootstrapNodes))
	for _, n := range config.BootstrapNodes {
		nd, err := node.ParseNode(n)
		if err != nil {
			d.local.Log.Warning("Could'nt parse bootstrap node string skpping str=%v, err=%v", n, err)
			// TODO : handle errors
			continue
		}
		bn = append(bn, nd)
	}
	//TODO: Return err if no bootstrap nodes were parsed.
	d.bootstrapper = newRefresher(d.rt, d.disc, bn, ln.Log)

	return d
}

// SetLocalAddresses sets the local addresses to be advertised.
func (d *Discovery) SetLocalAddresses(tcp, udp int) {
	d.disc.SetLocalAddresses(tcp, udp)
}

// Remove removes a record from the routing table
func (d *Discovery) Remove(key p2pcrypto.PublicKey) {
	d.rt.RemoveAddress(key) // we don't care about address when we remove
}

func (d *Discovery) Bootstrap(ctx context.Context) error {
	d.local.Debug("Starting node bootstrap ", d.local.String())
	return d.bootstrapper.Bootstrap(ctx, d.config.RandomConnections)
}
