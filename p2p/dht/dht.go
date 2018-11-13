// Package dht implements a Distributed Hash Table based on Kademlia protocol.
package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"

	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

// DHT is an interface to a general distributed hash table.
type DHT interface {
	Update(node node.Node)
	Lookup(pubkey string) (node.Node, error)

	SelectPeers(qty int) []node.Node
	Bootstrap() error

	Size() int
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

	rt  RoutingTable
	fnp *findNodeProtocol

	service service.Service
}

func (d *KadDHT) Size() int {
	req := make(chan int)
	d.rt.Size(req)
	return <-req
}

func (d *KadDHT) SelectPeers(qty int) []node.Node {
	return d.rt.SelectPeers(qty)
}

// New creates a new dht
func New(node *node.LocalNode, config config.SwarmConfig, service service.Service) *KadDHT {
	d := &KadDHT{
		config:  config,
		local:   node,
		rt:      NewRoutingTable(config.RoutingTableBucketSize, node.DhtID(), node.Logger),
		service: service,
	}
	d.fnp = newFindNodeProtocol(service, d.rt)
	return d
}

// Update insert or update a node in the routing table.
func (d *KadDHT) Update(node node.Node) {
	d.rt.Update(node)
}

// Lookup finds a node in the dht by its public key, it issues a search inside the local routing table,
// if the node can't be found there it sends a query to the network.
func (d *KadDHT) Lookup(pubkey string) (node.Node, error) {
	dhtid := node.NewDhtIDFromBase58(pubkey)
	poc := make(PeersOpChannel)
	d.rt.NearestPeers(NearestPeersReq{dhtid, d.config.RoutingTableAlpha, poc})
	res := (<-poc).Peers
	if len(res) == 0 {
		return node.EmptyNode, ErrEmptyRoutingTable
	}

	if res[0].DhtID().Equals(dhtid) {
		return res[0], nil
	}

	return d.kadLookup(pubkey, res)
}

// Implements the kad algo for locating a remote node
// Precondition - node is not in local routing table
// nodeId: - base58 node id string
// Returns requested node via the callback or nil if not found
// Also used as a bootstrap function to populate the routing table with the results.
func (d *KadDHT) kadLookup(id string, searchList []node.Node) (node.Node, error) {
	// save queried node ids for the operation
	queried := map[string]struct{}{}

	// iterative lookups for nodeId using searchList

	for {
		// if no new nodes found
		if len(searchList) == 0 {
			break
		}

		// is closestNode out target ?
		closestNode := searchList[0]
		if closestNode.PublicKey().String() == id {
			return closestNode, nil
		}

		// pick up to alpha servers to query from the search list
		// servers that have been recently queried will not be returned
		servers := filterFindNodeServers(searchList, queried, d.config.RoutingTableAlpha)

		if len(servers) == 0 {
			// no more servers to query
			// target node was not found.
			return node.EmptyNode, ErrLookupFailed
		}

		// lookup nodeId using the target servers
		res := d.findNodeOp(servers, queried, id, closestNode)
		if len(res) > 0 {

			// merge newly found nodes
			searchList = node.Union(searchList, res)
			// sort by distance from target
			searchList = node.SortByDhtID(res, node.NewDhtIDFromBase58(id))
		}
		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}

	return node.EmptyNode, ErrLookupFailed
}

// filterFindNodeServers picks up to count server who haven't been queried recently.
func filterFindNodeServers(nodes []node.Node, queried map[string]struct{}, alpha int) []node.Node {

	// If no server have been queried already, just make sure the list len is alpha
	if len(queried) == 0 {
		if len(nodes) > alpha {
			nodes = nodes[:alpha]
		}
		return nodes
	}

	// filter out queried servers.
	i := 0
	for _, v := range nodes {
		if _, exist := queried[v.String()]; exist {
			continue
		}

		nodes[i] = v
		i++

		if i >= alpha {
			break
		}
	}

	return nodes[:i]
}

// findNodeOp a target node on one or more servers
// returns closest nodes which are closers than closestNode to targetId
// if node found it will be in top of results list
func (d *KadDHT) findNodeOp(servers []node.Node, queried map[string]struct{}, id string, closestNode node.Node) []node.Node {
	l := len(servers)

	if l == 0 {
		return []node.Node{}
	}

	// results channel
	results := make(chan []node.Node)

	// Issue a parallel FindNode op to all servers on the list
	for i := 0; i < l; i++ {
		server := servers[i]
		queried[server.String()] = struct{}{}
		idx := id
		// find node protocol adds found nodes to the local routing table
		// populates queried node's routing table with us and return.
		go func(server node.Node, id string) {
			fnd, err := d.fnp.FindNode(server, id)
			if err != nil {
				//TODO: handle errors
				return
			}
			results <- fnd
		}(server, idx)
	}

	done := 0                          // To know when all operations finished
	idSet := make(map[string]struct{}) // to remove duplicates

	out := make([]node.Node, 0) // the end result we collect

	timeout := time.NewTimer(LookupTimeout)
Loop:
	for {
		select {
		case res := <-results:

			for _, n := range res {

				if _, ok := idSet[n.PublicKey().String()]; ok {
					continue
				}
				idSet[n.PublicKey().String()] = struct{}{}

				d.rt.Update(n)
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
