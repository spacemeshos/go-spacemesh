// Package dht implements a Distributed Hash Table based on Kademlia protocol.
package dht

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

// DHT is an interface to a general distributed hash table.
type Discover interface {
	Remove(pubkey p2pcrypto.PublicKey)
	Lookup(pubkey p2pcrypto.PublicKey) (node.Node, error)
	Update(addr, src node.Node)
	SelectPeers(qty int) []node.Node
	Bootstrap(ctx context.Context) error
	Size() int
	SetLocalAddresses(tcp, udp string)
}

type Protocol interface {
	Ping(p p2pcrypto.PublicKey) error
	GetAddresses(server p2pcrypto.PublicKey) ([]discNode, error)
	SetLocalAddresses(tcp, udp string)
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
func (d *Discovery) SelectPeers(qty int) []node.Node {
	out := make([]node.Node, 0, qty)
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
		out = append(out, add.DiscNode().Node)
		set[add.DiscNode().PublicKey()] = struct{}{}
	}
	return out
}

// Lookup searched a node in the address book. *NOTE* this returns a `Node` with the udpAddress as `Address()`.
// this is because Lookup is only used in the udp mux.
func (d *Discovery) Lookup(key p2pcrypto.PublicKey) (node.Node, error) {
	l, err := d.rt.Lookup(key)
	if err != nil {
		return node.EmptyNode, err
	}
	return node.New(key, l.udpAddress), nil
}

// Update adds an addr to the addrBook
func (d *Discovery) Update(addr, src node.Node) {
	d.rt.AddAddress(discNodeFromNode(addr, addr.Address()), discNodeFromNode(src, src.Address()))
}

// New creates a new Discovery
func New(ln *node.LocalNode, config config.SwarmConfig, service server.Service) *Discovery {
	d := &Discovery{
		config: config,
		local:  ln,
		rt:     NewAddrBook(discNodeFromNode(ln.Node, ln.Node.Address()), config, ln.Log),
	}

	d.disc = NewDiscoveryProtocol(ln.Node, d.rt, service, ln.Log)

	d.bootstrapper = newRefresher(d.rt, d.disc, config, ln.Log)

	return d
}

// SetLocalAddresses sets the local addresses to be advertised.
func (d *Discovery) SetLocalAddresses(tcp, udp string) {
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
