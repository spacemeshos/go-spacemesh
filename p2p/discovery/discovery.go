// Package discovery implements uses bitcoin-based addrbook to store network addresses and collects
// them by crawling the network using a simple protocol.
package discovery

import (
	"context"
	"errors"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

// PeerStore is an interface to the discovery protocol
type PeerStore interface {
	Remove(pubkey p2pcrypto.PublicKey)
	Lookup(pubkey p2pcrypto.PublicKey) (*node.Info, error)
	Update(addr, src *node.Info)

	SelectPeers(ctx context.Context, qty int) []*node.Info
	Bootstrap(ctx context.Context) error
	Size() int

	Shutdown()

	IsLocalAddress(info *node.Info) bool
	SetLocalAddresses(tcp, udp int)

	Good(key p2pcrypto.PublicKey)
	Attempt(key p2pcrypto.PublicKey)
}

// Protocol is the API of node messages used to discover new nodes.
type Protocol interface {
	Ping(context.Context, p2pcrypto.PublicKey) error
	GetAddresses(context.Context, p2pcrypto.PublicKey) ([]*node.Info, error)
	SetLocalAddresses(tcp, udp int)
	Close()
}

type addressBook interface {
	Good(key p2pcrypto.PublicKey)
	Attempt(key p2pcrypto.PublicKey)

	RemoveAddress(key p2pcrypto.PublicKey)
	AddAddress(addr, srcAddr *node.Info)
	AddAddresses(addrs []*node.Info, srcAddr *node.Info)

	NeedNewAddresses() bool
	Lookup(key p2pcrypto.PublicKey) (*node.Info, error)
	AddressCache() []*node.Info
	NumAddresses() int
	GetAddress() *KnownAddress

	AddLocalAddress(info *node.Info)
	IsLocalAddress(info *node.Info) bool

	Start()
	Stop()
}

type bootstrapper interface {
	Bootstrap(ctx context.Context, minPeers int) error
}

var (
	// ErrLookupFailed determines that we could'nt lookup this node in the routing table or network
	ErrLookupFailed = errors.New("failed to lookup node in the network")
)

// Discovery is struct that holds the protocol components, the protocol definition, the addr book data structure and more.
type Discovery struct {
	logger log.Log
	config config.SwarmConfig

	disc Protocol

	local        node.LocalNode
	rt           addressBook
	bootstrapper bootstrapper
}

// Size returns the size of addrBook.
func (d *Discovery) Size() int {
	return d.rt.NumAddresses()
}

// Good marks a node as good in the addrBook.
func (d *Discovery) Good(key p2pcrypto.PublicKey) {
	d.rt.Good(key)
}

// Attempt marks an attempt on the node in the addrBook
func (d *Discovery) Attempt(key p2pcrypto.PublicKey) {
	d.rt.Attempt(key)
}

func (d *Discovery) refresh(ctx context.Context, peersToGet int) error {
	if err := d.bootstrapper.Bootstrap(ctx, peersToGet); err != nil {
		d.logger.With().Error("addrbook refresh error", log.Err(err))
		return err
	}
	return nil
}

// SelectPeers asks routing table to randomly select a slice of nodes in size `qty`
func (d *Discovery) SelectPeers(ctx context.Context, qty int) []*node.Info {
	if d.rt.NeedNewAddresses() {
		err := d.refresh(ctx, qty) // TODO: use ctx with timeout, check errors
		if err == ErrBootAbort {
			return nil
		}
	}

	out := make([]*node.Info, 0, qty)
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
		out = append(out, add.NodeInfo())
		set[add.NodeInfo().PublicKey()] = struct{}{}
	}
	return out
}

// Lookup searched a node in the address book. *NOTE* this returns a `Node` with the udpAddress as `Address()`.
// this is because Lookup is only used in the udp mux.
func (d *Discovery) Lookup(key p2pcrypto.PublicKey) (*node.Info, error) {
	return d.rt.Lookup(key)
}

// Update adds an addr to the addrBook
func (d *Discovery) Update(addr, src *node.Info) {
	d.rt.AddAddress(addr, src)
}

// New creates a new Discovery
func New(ctx context.Context, ln node.LocalNode, config config.SwarmConfig, service server.Service, path string, logger log.Log) *Discovery {
	d := &Discovery{
		config: config,
		logger: logger,
		local:  ln,
		rt:     newAddrBook(config, path, logger),
	}

	d.rt.Start()
	d.rt.AddLocalAddress(&node.Info{ID: ln.PublicKey().Array()})

	d.disc = newProtocol(ctx, ln.PublicKey(), d.rt, service, logger)

	bn := make([]*node.Info, 0, len(config.BootstrapNodes))
	for _, n := range config.BootstrapNodes {
		nd, err := node.ParseNode(n)
		if err != nil {
			d.logger.WithContext(ctx).With().Warning("couldn't parse bootstrap node string, skipping",
				log.String("bootstrap_node_string", n),
				log.Err(err))
			// TODO : handle errors
			continue
		}
		bn = append(bn, nd)
	}

	//TODO: Return err if no bootstrap nodes were parsed.
	d.bootstrapper = newRefresher(ln.PublicKey(), d.rt, d.disc, bn, logger)

	return d
}

// Shutdown stops the discovery service
func (d *Discovery) Shutdown() {
	d.rt.Stop()
}

// IsLocalAddress checks info against the addrBook to see if it's a local address.
func (d *Discovery) IsLocalAddress(info *node.Info) bool {
	return d.rt.IsLocalAddress(info)
}

// SetLocalAddresses sets the localNode addresses to be advertised.
func (d *Discovery) SetLocalAddresses(tcp, udp int) {
	d.disc.SetLocalAddresses(tcp, udp)
	//TODO: lookup a protocol or just pass here our IP to the routing table
}

// Remove removes a record from the routing table
func (d *Discovery) Remove(key p2pcrypto.PublicKey) {
	d.rt.RemoveAddress(key) // we don't care about address when we remove
}

// Bootstrap runs a refresh and tries to get a minimum number of nodes in the addrBook.
func (d *Discovery) Bootstrap(ctx context.Context) error {
	d.logger.Debug("starting node bootstrap")
	return d.refresh(ctx, d.config.RandomConnections)
}
