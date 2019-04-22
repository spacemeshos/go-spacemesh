package dht

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"time"
)

const (
	// BootstrapTimeout is the maximum time we allow the bootstrap process to extend
	BootstrapTimeout = 5 * time.Minute
	// LookupIntervals is the time we wait between another kad lookup if bootstrap failed.
	LookupIntervals = 100 * time.Millisecond
	// RefreshInterval is the time we wait between dht refreshes
	RefreshInterval = 5 * time.Minute
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
// ID with a FindNode (using `dht.netLookup`). the process involves updating all returned nodes to the routing table
// while all the nodes that receive our query will add us to their routing tables and send us as response to a `FindNode`.
func (d *KadDHT) Bootstrap(ctx context.Context) error {

	d.local.Debug("Starting node bootstrap ", d.local.String())

	alpha := d.config.RoutingTableAlpha
	c := d.config.RandomConnections

	if c <= 0 || alpha <= 0 {
		return ErrZeroConnections
	}
	// register bootstrap nodes
	bn := make([]discNode, 0, len(d.config.BootstrapNodes))
	for _, n := range d.config.BootstrapNodes {
		nd, err := node.NewNodeFromString(n)
		if err != nil {
			// TODO : handle errors
			continue
		}
		bn = append(bn, discNode{nd, nd.Address()})
		d.local.Info("added new bootstrap node %v", nd)
	}

	if len(bn) == 0 {
		return ErrConnectToBootNode
	}

	d.local.Debug("lookup using %d preloaded bootnodes ", len(bn))

	err := d.tryBoot(ctx, bn, c)
	return err
}

func (d *KadDHT) tryBoot(ctx context.Context, bootnodes []discNode, minPeers int) error {

	searchFor := d.local.PublicKey()
	servers := make([]discNode, len(bootnodes))
	copy(servers, bootnodes)

	tries := 0
	size := 0

loop:
	for {
		reschan := make(chan error)

		go func() {
			if tries > 0 {
				//TODO: consider choosing a random key that is close to the local id
				//or TODO: implement real kademlia refreshes - #241
				searchFor = p2pcrypto.NewRandomPubkey()
				servers = d.internalLookup(searchFor)
				// TODO: prioritize new nodes to reduce double checking
				if len(servers) == 0 {
					servers = append(servers, bootnodes...)
				}
			}
			d.local.Info("BOOTSTRAP: peer lookup w/ %v servers", len(servers))
			_, err := d.kadLookup(searchFor, servers)
			reschan <- err
		}()

		select {
		case <-ctx.Done():
			return ErrBootAbort
		case err := <-reschan:
			tries++
			if err == nil {
				d.local.Log.Warning("Found node in bootstrap lookup (%v)", searchFor.String())
				// if we got the peer we were looking for (us or random)
				// the best thing we can do is just try again or try another random peer.
				// hence we continue here.
			}
			req := make(chan int)
			d.rt.Size(req)
			size = <-req

			if size > 0 && size-len(bootnodes) >= minPeers {
				break loop
			}
		}

		d.local.Warning("%d lookup didn't bootstrap the routing table. RT now has %d peers", tries, size)
		timer := time.NewTimer(time.Duration(tries) * LookupIntervals) // todo: maybe wait only when size didn't increased. (to let nodes populate)
		select {
		case <-ctx.Done():
			return ErrBootAbort
		case <-timer.C:
			continue loop
		}

	}

	// we don't need bootstrap node anymore
	for i := 0; i < len(bootnodes); i++ {
		d.rt.Remove(bootnodes[i])
	}

	return nil
}
