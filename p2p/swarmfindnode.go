package p2p

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"time"
)

// LookupTimeout is the timeout we allow for a single FindNode operation
const LookupTimeout = 60 * time.Second

// Finds a node based on its id - internal method
// id: base58 node id string
// returns remote node or nil when not found
// not go safe - should only be called from swarm event dispatcher
func (s *swarmImpl) findNode(id string, callback chan node.Node) {

	s.localNode.Debug("finding node: %s ...", log.PrettyID(id))

	// look for the node at local dht table
	poc := make(dht.PeerOpChannel, 1)
	s.routingTable.Find(dht.PeerByIDRequest{ID: node.NewDhtIDFromBase58(id), Callback: poc})
	select {
	case c := <-poc:
		res := c.Peer
		if res != node.EmptyNode {
			go func() { callback <- res }()
			return
		}
	}

	// use kad to locate the node
	s.kadFindNode(id, callback)
}

// FilterFindNodeServers picks up to count server who haven't been queried recently.
func FilterFindNodeServers(nodes []node.Node, queried map[string]struct{}, alpha int) []node.Node {

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
		if _, exist := queried[v.String()]; !exist {
			nodes[i] = v
			i++
		}
		if i >= alpha {
			break
		}
	}

	return nodes[:i]
}

// Implements the kad algo for locating a remote node
// Precondition - node is not in local routing table
// nodeId: - base58 node id string
// Returns requested node via the callback or nil if not found
// Also used as a bootstrap function to populate the routing table with the results.
func (s *swarmImpl) kadFindNode(nodeID string, callback chan node.Node) {

	s.localNode.Debug("Kad find node: %s ...", nodeID)

	// kad node location algo
	alpha := int(s.config.SwarmConfig.RoutingTableAlpha)
	dhtID := node.NewDhtIDFromBase58(nodeID)

	// step 1 - get up to alpha closest nodes to the target in the local routing table
	searchList := s.getNearestPeers(dhtID, alpha)

	// Every node is queried only once per operation.
	queried := map[string]struct{}{}

	// step 2 - iterative lookups for nodeId using searchList

Loop:
	for {
		// if no new nodes found
		if len(searchList) == 0 {
			go func() { callback <- node.EmptyNode }()
			break Loop
		}

		// is closestNode out target ?
		closestNode := searchList[0]
		if closestNode.String() == nodeID {
			go func() { callback <- closestNode }()
			break Loop
		}

		// pick up to alpha servers to query from the search list
		// servers that have been recently queried will not be returned
		servers := FilterFindNodeServers(searchList, queried, alpha)

		if len(servers) == 0 {
			// no more servers to query
			// target node was not found.
			go func() { callback <- node.EmptyNode }()
			break Loop
		}

		// lookup nodeId using the target servers
		res := s.lookupNode(servers, queried, nodeID, closestNode)
		if len(res) > 0 {

			// merge newly found nodes
			searchList = node.Union(searchList, res)

			// sort by distance from target
			searchList = node.SortByDhtID(res, dhtID)
		}

		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}
	s.localNode.Debug("FindNode(%v) finished", nodeID)
}

// Lookup a target node on one or more servers
// Returns closest nodes which are closers than closestNode to targetId
// If node found it will be in top of results list
func (s *swarmImpl) lookupNode(servers []node.Node, queried map[string]struct{}, targetID string, closestNode node.Node) []node.Node {

	l := len(servers)

	if l == 0 {
		return []node.Node{}
	}

	// results channel
	callback := make(chan FindNodeResp, l)

	// Issue a parallel FindNode op to all servers on the list
	for i := 0; i < l; i++ {
		queried[servers[i].String()] = struct{}{}
		// find node protocol adds found nodes to the local routing table
		// populates queried node's routing table with us and return.
		go s.getFindNodeProtocol().FindNode(crypto.UUID(), servers[i].String(), targetID, callback)
	}

	done := 0
	idSet := make(map[string]node.Node)
	timeout := time.NewTimer(LookupTimeout)
Loop:
	for {
		select {
		case res := <-callback:
			nodes := node.FromNodeInfos(res.NodeInfos)
			for _, n := range nodes {
				idSet[n.String()] = n
			}

			done++
			s.localNode.Debug("FindNode %d/%d queries returned.", done, l)
			if done == l {
				break Loop
			}

		case <-timeout.C:
			// we expected nodes to return results within a reasonable time frame
			s.localNode.Debug("FindNode was not returned within timelimit")
			break Loop
		}
	}

	// add unique node ids that are closer to target id than closest node
	res := make([]node.Node, len(idSet))
	i := 0
	for _, n := range idSet {
		res[i] = n
		i++
	}

	// sort results by distance from target dht id
	return res
}

// helper method - a sync wrapper over routingTable.NearestPeers
func (s *swarmImpl) getNearestPeers(dhtID node.DhtID, count int) []node.Node {
	psoc := make(dht.PeersOpChannel, 1)
	s.routingTable.NearestPeers(dht.NearestPeersReq{ID: dhtID, Count: count, Callback: psoc})
	res := <-psoc
	set := make([]node.Node, len(res.Peers))
	for i := range res.Peers {
		set[i] = res.Peers[i]
	}
	return set
}
