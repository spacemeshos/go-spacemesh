// Package dht implements a Distributed Hash Table based on Kademlia protocol.
package dht

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"time"
)

// DHT is an interface to a general distributed hash table.
type DHT interface {
	Remove(p node.Node)
	Lookup(pubkey p2pcrypto.PublicKey) (node.Node, error)
	SelectPeers(qty int) []node.Node
	Bootstrap(ctx context.Context) error
	Size() int
	SetLocalAddresses(tcp, udp string)
}

// LookupTimeout is the timelimit we give to a single lookup operation
const LookupTimeout = 15 * time.Second

var (
	// ErrLookupFailed determines that we could'nt find this node in the routing table or network
	ErrLookupFailed = errors.New("failed to find node in the network")
	// ErrEmptyRoutingTable means that our routing table is empty thus we can't find any node (so we can't query any)
	ErrEmptyRoutingTable = errors.New("no nodes to query - routing table is empty")
)

// KadDHT represents the Distributed Hash Table, it holds the Routing Table local node cache. and a FindNode kademlia protocol.
// KadDHT Is created with a localNode identity as base. (DhtID)
type KadDHT struct {
	config config.SwarmConfig

	local *node.LocalNode

	rt RoutingTable

	disc DiscoveryProtocol
}

// Size returns the size of the routing table.
func (d *KadDHT) Size() int {
	req := make(chan int)
	d.rt.Size(req)
	return <-req
}

// SelectPeers asks routing table to randomly select a slice of nodes in size `qty`
func (d *KadDHT) SelectPeers(qty int) []node.Node {
	regpeers := d.rt.SelectPeers(qty)
	size := len(regpeers)
	tcpnodes := make([]node.Node, size)
	for i := 0; i < size; i++ {
		tcpnodes[i] = regpeers[i].Node
	}
	return tcpnodes
}

func (d *KadDHT) Lookup(key p2pcrypto.PublicKey) (node.Node, error) {
	res := d.internalLookup(key)
	if res == nil || len(res) == 0 {
		return node.EmptyNode, errors.New("coudld'nt find requested key node")
	}

	if res[0].PublicKey().String() != key.String() {
		return node.EmptyNode, errors.New("coudld'nt find requested key node")
	}
	return node.New(key, res[0].udpAddress), nil
}

type DiscoveryProtocol interface {
	Ping(p p2pcrypto.PublicKey) error
	FindNode(server p2pcrypto.PublicKey, target p2pcrypto.PublicKey) ([]discNode, error)
	SetLocalAddresses(tcp, udp string)
}

// New creates a new dht
func New(ln *node.LocalNode, config config.SwarmConfig, service server.Service) *KadDHT {
	d := &KadDHT{
		config: config,
		local:  ln,
		rt:     NewRoutingTable(config.RoutingTableBucketSize, ln.DhtID(), ln.Log),
	}

	d.disc = NewDiscoveryProtocol(ln.Node, d, service, ln.Log)

	return d
}

func (d *KadDHT) SetLocalAddresses(tcp, udp string) {
	d.disc.SetLocalAddresses(tcp, udp)
}

// Update insert or updates a node in the routing table.
func (d *KadDHT) Update(n discNode) {
	d.rt.Update(n)
}

// Remove removes a record from the routing table
func (d *KadDHT) Remove(n node.Node) {
	d.rt.Remove(discNode{n, ""}) // we don't care about address when we remove
}

// netLookup finds a node in the dht by its public key, it issues a search inside the local routing table,
// if the node can't be found there it sends a query to the network.
func (d *KadDHT) netLookup(pubkey p2pcrypto.PublicKey) (node.Node, error) {
	res := d.internalLookup(pubkey)

	if res == nil || len(res) < 1 {
		return node.EmptyNode, errors.New("no peers found in routing table")
	}

	if res[0].PublicKey().String() == pubkey.String() {
		return node.New(res[0].PublicKey(), res[0].udpAddress), nil
	}

	lres, err := d.kadLookup(pubkey, res)
	if err != nil {
		// always ?
		return node.EmptyNode, err
	}
	return node.New(lres.PublicKey(), lres.udpAddress), nil
}

// internalLookup finds a node in the dht by its public key, it issues a search inside the local routing table
// *NOTE* = currently, lookup happens only on bootstrap so it returns a  works
func (d *KadDHT) internalLookup(key p2pcrypto.PublicKey) []discNode {
	poc := make(PeersOpChannel)
	dhtid := node.NewDhtID(key.Bytes())
	d.rt.NearestPeers(NearestPeersReq{dhtid, d.config.RoutingTableBucketSize, poc})
	res := (<-poc).Peers
	if len(res) == 0 {
		return nil
	}
	return res
}

// Implements the kad algo for locating a remote node
// Precondition - node is not in local routing table
// nodeId: - base58 node id string
// Returns requested node via the callback or nil if not found
// Also used as a bootstrap function to populate the routing table with the results.
func (d *KadDHT) kadLookup(id p2pcrypto.PublicKey, searchList []discNode) (discNode, error) {
	// save queried node ids for the operation
	queried := make(map[discNode]bool)

	// iterative lookups for nodeId using searchList

	for {
		// if no new nodes found
		if len(searchList) == 0 {
			break
		}

		// is closestNode out target ?
		closestNode := searchList[0]
		if closestNode.PublicKey().String() == id.String() {
			return closestNode, nil
		}

		// pick up to alpha servers to query from the search list
		// servers that have been recently queried will not be returned
		servers := filterFindNodeServers(searchList, queried, d.config.RoutingTableAlpha)

		if len(servers) == 0 {
			// no more servers to query
			// target node was not found.
			return emptyDiscNode, ErrLookupFailed
		}

		probed := 0
		for _, active := range queried {
			if active {
				probed++
			}

			if probed >= d.config.RoutingTableBucketSize {
				return emptyDiscNode, ErrLookupFailed // todo: maybe just return what we have
			}
		}

		// lookup nodeId using the target servers
		res := d.findNodeOp(servers, queried, id)
		if len(res) > 0 {
			// merge newly found nodes
			searchList = Union(searchList, res)
			// sort by distance from target
			searchList = SortByDhtID(res, node.NewDhtID(id.Bytes()))
		}
		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}

	return emptyDiscNode, ErrLookupFailed
}

// filterFindNodeServers picks up to count server who haven't been queried recently.
func filterFindNodeServers(nodes []discNode, queried map[discNode]bool, alpha int) []discNode {

	// If no server have been queried already, just make sure the list len is alpha
	if len(queried) == 0 {
		if len(nodes) > alpha {
			nodes = nodes[:alpha]
		}
		return nodes
	}

	newlist := make([]discNode, 0, len(nodes))

	// filter out queried servers.
	i := 0
	for _, v := range nodes {
		if _, exist := queried[v]; exist {
			continue
		}

		newlist = append(newlist, v)
		i++

		if i == alpha {
			break
		}
	}

	return newlist
}

type findNodeOpRes struct {
	res    []discNode
	server discNode
}

// findNodeOp a target node on one or more servers
// returns closest nodes which are closers than closestNode to targetId
// if node found it will be in top of results list
func (d *KadDHT) findNodeOp(servers []discNode, queried map[discNode]bool, id p2pcrypto.PublicKey) []discNode {

	var out []discNode
	startTime := time.Now()

	defer func() {
		d.local.With().Debug("findNodeOp", log.Int("servers", len(servers)), log.Int("result_count", len(out)), log.Duration("time_elapsed", time.Since(startTime)))
	}()

	l := len(servers)

	if l == 0 {
		return []discNode{}
	}

	results := make(chan *findNodeOpRes)

	// todo : kademlia says: This recursion can begin before all of the previous RPCs have returned
	// currently we wait for all.

	// Issue a parallel FindNode op to all servers on the list
	for i := 0; i < l; i++ {

		// find node protocol adds found nodes to the local routing table
		// populates queried node's routing table with us and return.
		go func(server discNode, idx p2pcrypto.PublicKey) {
			d.rt.Update(server) // must do this prior to message to make sure we have that node

			var err error

			defer func() {
				if err != nil {
					d.local.With().Debug("find_node_failed", log.String("server", server.String()), log.Err(err))
					results <- &findNodeOpRes{nil, server}
					d.rt.Remove(server) // remove node if it didn't work
					return
				}
			}()

			err = d.disc.Ping(server.PublicKey())
			if err != nil {
				return
			}

			fnd, err := d.disc.FindNode(server.PublicKey(), idx)
			if err != nil {
				return
			}

			results <- &findNodeOpRes{fnd, server}
		}(servers[i], id)
	}

	done := 0                          // To know when all operations finished
	idSet := make(map[string]struct{}) // to remove duplicates

	timeout := time.NewTimer(LookupTimeout)
Loop:
	for {
		select {
		case qres := <-results:

			// we mark active nodes
			queried[qres.server] = qres.res != nil

			res := qres.res
			for _, n := range res {

				if _, ok := idSet[n.PublicKey().String()]; ok {
					continue
				}
				idSet[n.PublicKey().String()] = struct{}{}

				if _, ok := queried[n]; ok {
					continue
				}
				out = append(out, n)
			}

			done++
			if done == l {
				close(results)
				break Loop
			}
		case <-timeout.C:
			// we expected nodes to return results within a reasonable time frame
			// we return what we have now.
			break Loop
		}
	}

	return out
}
