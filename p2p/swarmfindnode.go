package p2p

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"time"
)

// LookupTimeout is the timeout we allow for a single FindNode operation
const LookupTimeout = 60 * time.Second

// Finds a node based on its id - internal method
// id: base58 node id string
// returns remote node or nil when not found
// not go safe - should only be called from swarm event dispatcher
func (s *swarmImpl) findNode(id string, callback chan node.RemoteNodeData) {

	s.localNode.Info("finding node: %s ...", log.PrettyID(id))

	// look for the node at local dht table
	poc := make(table.PeerOpChannel, 1)
	s.routingTable.Find(table.PeerByIDRequest{ID: dht.NewIDFromBase58String(id), Callback: poc})
	select {
	case c := <-poc:
		res := c.Peer
		if res != nil {
			go func() { callback <- res }()
			return
		}
	}

	// use kad to locate the node
	s.kadFindNode(id, callback)
}

// FilterFindNodeServers picks up to count server who haven't been queried recently.
func FilterFindNodeServers(nodes []node.RemoteNodeData, queried map[string]struct{}, alpha int) []node.RemoteNodeData {

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
		if _, exist := queried[v.ID()]; !exist {
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
func (s *swarmImpl) kadFindNode(nodeID string, callback chan node.RemoteNodeData) {

	s.localNode.Debug("Starting Kad FindNode(%v)", nodeID)

	// kad node location algo
	alpha := int(s.config.RoutingTableAlpha)
	dhtID := dht.NewIDFromBase58String(nodeID)

	// step 1 - get up to alpha closest nodes to the target in the local routing table
	searchList := s.getNearestPeers(dhtID, alpha)

	// Every node is queried only once per operation.
	queried := map[string]struct{}{}

	// step 2 - iterative lookups for nodeId using searchList

Loop:
	for {
		// if no new nodes found
		if len(searchList) == 0 {
			go func() { callback <- nil }()
			break Loop
		}

		// is closestNode out target ?
		closestNode := searchList[0]
		if closestNode.ID() == nodeID {
			go func() { callback <- closestNode }()
			break Loop
		}

		// pick up to alpha servers to query from the search list
		// servers that have been recently queried will not be returned
		servers := FilterFindNodeServers(searchList, queried, alpha)

		if len(servers) == 0 {
			// no more servers to query
			// target node was not found.
			go func() { callback <- nil }()
			break Loop
		}

		// lookup nodeId using the target servers
		res := s.lookupNode(servers, queried, nodeID, closestNode)
		if len(res) > 0 {

			// merge newly found nodes
			searchList = node.Union(searchList, res)

			// sort by distance from target
			searchList = node.SortClosestPeers(res, dhtID)
		}

		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}
	s.localNode.Debug("FindNode(%v) finished", nodeID)
}

// Lookup a target node on one or more servers
// Returns closest nodes which are closers than closestNode to targetId
// If node found it will be in top of results list
func (s *swarmImpl) lookupNode(servers []node.RemoteNodeData, queried map[string]struct{}, targetID string, closestNode node.RemoteNodeData) []node.RemoteNodeData {

	l := len(servers)

	if l == 0 {
		return []node.RemoteNodeData{}
	}

	// results channel
	callback := make(chan FindNodeResp, l)

	// Issue a parallel FindNode op to all servers on the list
	for i := 0; i < l; i++ {
		queried[servers[i].ID()] = struct{}{}
		// find node protocol adds found nodes to the local routing table
		// populates queried node's routing table with us and return.
		go s.getFindNodeProtocol().FindNode(crypto.UUID(), servers[i].ID(), targetID, callback)
	}

	done := 0
	idSet := make(map[string]node.RemoteNodeData)
	timeout := time.NewTimer(LookupTimeout)
Loop:
	for {
		select {
		case res := <-callback:
			nodes := node.FromNodeInfos(res.NodeInfos)
			for _, n := range nodes {
				idSet[n.ID()] = n
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
	res := make([]node.RemoteNodeData, len(idSet))
	i := 0
	for _, n := range idSet {
		res[i] = n
		i++
	}

	// sort results by distance from target dht id
	return res
}

// helper method - a sync wrapper over routingTable.NearestPeers
func (s *swarmImpl) getNearestPeers(dhtID dht.ID, count int) []node.RemoteNodeData {
	psoc := make(table.PeersOpChannel, 1)
	s.routingTable.NearestPeers(table.NearestPeersReq{ID: dhtID, Count: count, Callback: psoc})
	return (<-psoc).Peers
}
