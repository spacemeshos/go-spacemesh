package dht

import (
	"context"
	"errors"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"time"
)

const (
	// BootstrapTimeout is the maximum time we allow the bootstrap process to extend
	BootstrapTimeout = 5 * time.Minute
	// LookupIntervals is the time we wait between another kad lookup if bootstrap failed.
	LookupIntervals = 3 * time.Second
	// RefreshInterval is the time we wait between dht refreshes
	RefreshInterval = 5 * time.Minute

	BootstrapTries = 5
)

var (
	// ErrZeroConnections - we can't start the node without connecting
	ErrZeroConnections = errors.New("can't bootstrap minimum connections set to 0")
	// ErrConnectToBootNode is returned when a connection with the boot node is failed.
	ErrConnectToBootNode = errors.New("failed to read or connect to any boot node")
	// ErrFoundOurself is returned when a node sends us ourselves.
	ErrFoundOurself = errors.New("found ourselves in the routing table")
	// ErrFailedToBoot is returned when we exceed the BootstrapTimeout
	ErrFailedToBoot = errors.New("failed to bootstrap within time limit")
	// ErrBootAbort is returned when when bootstrap is canceled by context cancel
	ErrBootAbort = errors.New("Bootstrap canceled by signal")
)

// Bootstrap issues a bootstrap by inserting the preloaded nodes to the routing table then querying them with our
// ID with a FindNode (using `dht.Lookup`). the process involves updating all returned nodes to the routing table
// while all the nodes that receive our query will add us to their routing tables and send us as response to a `FindNode`.
func (d *KadDHT) Bootstrap(ctx context.Context) error {

	d.local.Debug("Starting node bootstrap ", d.local.String())

	alpha := d.config.RoutingTableAlpha
	c := d.config.RandomConnections

	if c <= 0 || alpha <= 0 {
		return ErrZeroConnections
	}
	// register bootstrap nodes
	bn := 0
	for _, n := range d.config.BootstrapNodes {
		node, err := node.NewNodeFromString(n)
		if err != nil {
			// TODO : handle errors
			continue
		}
		d.rt.Update(node)
		bn++
		d.local.Info("added new bootstrap node %v", node)
	}

	if bn == 0 {
		return ErrConnectToBootNode
	}

	d.local.Debug("lookup using %d preloaded bootnodes ", bn)

	ctx, _ = context.WithTimeout(ctx, BootstrapTimeout)
	err := d.tryBoot(ctx, c)

	return err
}

func (d *KadDHT) tryBoot(ctx context.Context, minPeers int) error {

	searchFor := d.local.PublicKey().String()
	booted := false
	i := 0
	d.local.Debug("BOOTSTRAP: Running kademlia lookup for ourselves")

loop:
	for {
		reschan := make(chan error)

		go func() {
			if booted || i >= BootstrapTries {
				rnd, _ := crypto.GetRandomBytes(32)
				searchFor = base58.Encode(rnd)
				d.local.Debug("BOOTSTRAP: Running kademlia lookup for random peer")
			}
			_, err := d.Lookup(searchFor)
			reschan <- err
		}()

		select {
		case <-ctx.Done():
			return ErrBootAbort
		case err := <-reschan:
			i++
			if err == nil {
				continue
			}
			// We want to have lookup failed error
			// no one should return us ourselves.
			req := make(chan int)
			d.rt.Size(req)
			size := <-req

			if (size) >= minPeers { // Don't count bootstrap nodes
				if booted {
					break loop
				}
				booted = true
			} else {
				d.local.Warning("%d lookup didn't bootstrap the routing table. RT now has %d peers", i, size)
			}

			time.Sleep(LookupIntervals)
		}
	}

	return nil
}
