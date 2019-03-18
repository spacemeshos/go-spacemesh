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
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/ping"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"net"
	"strings"
	"time"
)

// DHT is an interface to a general distributed hash table.
type DHT interface {
	Update(p node.Node)
	Remove(p node.Node)

	// todo: change lookup to be an internal lookup. (do not expose net lookup)
	InternalLookup(dhtid node.DhtID) []node.Node
	Lookup(pubkey p2pcrypto.PublicKey) (node.Node, error)

	SelectPeers(qty int) []node.Node
	Bootstrap(ctx context.Context) error

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

type Pinger interface {
	RegisterCallback(f func(from net.Addr, ping *pb.Ping) error)
	Ping(p p2pcrypto.PublicKey) error
}

// KadDHT represents the Distributed Hash Table, it holds the Routing Table local node cache. and a FindNode kademlia protocol.
// KadDHT Is created with a localNode identity as base. (DhtID)
type KadDHT struct {
	config config.SwarmConfig

	local *node.LocalNode

	rt RoutingTable

	fnp  *findNodeProtocol
	ping Pinger

	service service.Service
}

// Size returns the size of the routing table.
func (d *KadDHT) Size() int {
	req := make(chan int)
	d.rt.Size(req)
	return <-req
}

// SelectPeers asks routing table to randomly select a slice of nodes in size `qty`
func (d *KadDHT) SelectPeers(qty int) []node.Node {
	return d.rt.SelectPeers(qty)
}

// New creates a new dht
func New(ln *node.LocalNode, config config.SwarmConfig, service service.Service) *KadDHT {
	pinger := ping.New(ln.Node, service.(server.Service), ln.Log) // TODO : get from outside
	d := &KadDHT{
		config:  config,
		local:   ln,
		rt:      NewRoutingTable(config.RoutingTableBucketSize, ln.DhtID(), ln.Log),
		ping:    pinger,
		service: service,
	}
	d.fnp = newFindNodeProtocol(service, d.rt)

	pinger.RegisterCallback(d.PingerCallback)

	return d
}

func (d *KadDHT) PingerCallback(from net.Addr, p *pb.Ping) error {
	//todo: check the address provided with an extra ping before upading. ( if we haven't checked it for a while )
	k, err := p2pcrypto.NewPubkeyFromBytes(p.ID)

	if err != nil {
		return err
	}

	//extract port
	_, port, err := net.SplitHostPort(p.ListenAddress)
	if err != nil {
		return err
	}
	var addr string

	if spl := strings.Split(from.String(), ":"); len(spl) > 1 {
		addr, _, err = net.SplitHostPort(from.String())
	}

	if err != nil {
		return err
	}

	// todo: decide on best way to know our ext address
	d.rt.Update(node.New(k, net.JoinHostPort(addr, port)))
	return nil
}

// Update insert or updates a node in the routing table.
func (d *KadDHT) Update(p node.Node) {
	d.rt.Update(p)
}

// Remove removes a record from the routing table
func (d *KadDHT) Remove(p node.Node) {
	d.rt.Remove(p)
}

// Lookup finds a node in the dht by its public key, it issues a search inside the local routing table,
// if the node can't be found there it sends a query to the network.
func (d *KadDHT) Lookup(pubkey p2pcrypto.PublicKey) (node.Node, error) {
	dhtid := node.NewDhtID(pubkey.Bytes())

	res := d.InternalLookup(dhtid)

	if res == nil {
		return node.EmptyNode, errors.New("no peers found in routing table")
	}

	if res[0].DhtID().Equals(dhtid) {
		return res[0], nil
	}

	return d.kadLookup(pubkey, res)
}

// InternalLookup finds a node in the dht by its public key, it issues a search inside the local routing table
func (d *KadDHT) InternalLookup(dhtid node.DhtID) []node.Node {
	poc := make(PeersOpChannel)
	d.rt.NearestPeers(NearestPeersReq{dhtid, d.config.RoutingTableAlpha, poc})
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
func (d *KadDHT) kadLookup(id p2pcrypto.PublicKey, searchList []node.Node) (node.Node, error) {
	// save queried node ids for the operation
	queried := make(map[string]bool)

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
			return node.EmptyNode, ErrLookupFailed
		}

		probed := 0
		for _, active := range queried {
			if active {
				probed++
			}

			if probed >= d.config.RoutingTableBucketSize {
				return node.EmptyNode, ErrLookupFailed // todo: maybe just return what we have
			}
		}

		// lookup nodeId using the target servers
		res := d.findNodeOp(servers, queried, id)
		if len(res) > 0 {
			// merge newly found nodes
			searchList = node.Union(searchList, res)
			// sort by distance from target
			searchList = node.SortByDhtID(res, node.NewDhtID(id.Bytes()))
		}
		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}

	return node.EmptyNode, ErrLookupFailed
}

// filterFindNodeServers picks up to count server who haven't been queried recently.
func filterFindNodeServers(nodes []node.Node, queried map[string]bool, alpha int) []node.Node {

	// If no server have been queried already, just make sure the list len is alpha
	if len(queried) == 0 {
		if len(nodes) > alpha {
			nodes = nodes[:alpha]
		}
		return nodes
	}

	newlist := make([]node.Node, 0, len(nodes))

	// filter out queried servers.
	i := 0
	for _, v := range nodes {
		if _, exist := queried[v.PublicKey().String()]; exist {
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
	res    []node.Node
	server node.Node
}

// findNodeOp a target node on one or more servers
// returns closest nodes which are closers than closestNode to targetId
// if node found it will be in top of results list
func (d *KadDHT) findNodeOp(servers []node.Node, queried map[string]bool, id p2pcrypto.PublicKey) []node.Node {

	var out []node.Node
	startTime := time.Now()

	defer func() {
		d.local.With().Debug("findNodeOp", log.Int("servers", len(servers)), log.Int("result_count", len(out)), log.Duration("time_elapsed", time.Since(startTime)))
	}()

	l := len(servers)

	if l == 0 {
		return []node.Node{}
	}

	results := make(chan *findNodeOpRes)

	// todo : kademlia says: This recursion can begin before all of the previous RPCs have returned
	// currently we wait for all.

	// Issue a parallel FindNode op to all servers on the list
	for i := 0; i < l; i++ {

		// find node protocol adds found nodes to the local routing table
		// populates queried node's routing table with us and return.
		go func(server node.Node, idx p2pcrypto.PublicKey) {
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

			err = d.ping.Ping(server.PublicKey())
			if err != nil {
				return
			}

			fnd, err := d.fnp.FindNode(server, idx)
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
			queried[qres.server.String()] = qres.res != nil

			res := qres.res
			for _, n := range res {

				if _, ok := idSet[n.PublicKey().String()]; ok {
					continue
				}
				idSet[n.PublicKey().String()] = struct{}{}

				if _, ok := queried[n.PublicKey().String()]; ok {
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
