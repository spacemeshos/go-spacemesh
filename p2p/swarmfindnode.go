package p2p

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"time"
)

// Finds a node based on its id - internal method
// id: base58 node id string
// returns remote node or nil when not found
// not go safe - should only be called from swarm event dispatcher
func (s *swarmImpl) findNode(id string, callback chan node.RemoteNodeData) {

	s.localNode.Info("finding node: %s ...", log.PrettyID(id))

	// look at peer store
	n := s.peers[id]
	if n != nil {
		s.localNode.Info(" known peer")

		go func() { callback <- s.localNode.GetRemoteNodeData() }()
		return
	}

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

// Implements the kad algo for locating a remote node
// Precondition - node is not in local routing table
// nodeId: - base58 node id string
// Returns requested node via the callback or nil if not found
func (s *swarmImpl) kadFindNode(nodeID string, callback chan node.RemoteNodeData) {

	s.localNode.Info("Kad find node: %s ...", nodeID)

	// kad node location algo
	alpha := int(s.config.RoutingTableAlpha)
	k := int(s.config.RoutingTableBucketSize)
	dhtID := dht.NewIDFromBase58String(nodeID)

	// step 1 - get up to alpha closest nodes to the target in the local routing table
	searchList := s.getNearestPeers(dhtID, k)

	// step 2 - iterative lookups for nodeId using searchList

Loop:
	for {

		if len(searchList) == 0 {
			go func() { callback <- nil }()
			break Loop
		}

		closestNode := searchList[0]

		if closestNode.ID() == nodeID {
			go func() { callback <- closestNode }()
			break Loop
		}

		// pick up to alpha servers to query from the search list
		// servers that have been recently queried will not be returned
		servers := node.PickFindNodeServers(searchList, nodeID, alpha)

		if len(servers) == 0 {
			// no more servers to query
			go func() { callback <- nil }()
			break Loop
		}

		// lookup nodeId using the target servers
		res := s.lookupNode(servers, nodeID, closestNode)

		if len(res) > 0 {

			// merge newly found nodes
			searchList = node.Union(searchList, res)

			// sort by distance from target
			searchList = node.SortClosestPeers(res, dhtID)
		}

		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}
}

// Lookup a target node on one or more servers
// Returns closest nodes which are closers than closestNode to targetId
// If node found it will be in top of results list
func (s *swarmImpl) lookupNode(servers []node.RemoteNodeData, targetID string, closestNode node.RemoteNodeData) []node.RemoteNodeData {

	l := len(servers)

	if l == 0 {
		return []node.RemoteNodeData{}
	}

	// results channel
	callback := make(chan FindNodeResp, l)

	// queries are run in par and results are collected
	for i := 0; i < l; i++ {
		servers[i].SetLastFindNodeCall(targetID, time.Now())

		// find node protocol adds found nodes to the local routing table
		go s.getFindNodeProtocol().FindNode(crypto.UUID(), servers[i].ID(), targetID, callback)
	}

	done := 0
	idSet := make(map[string]node.RemoteNodeData)

Loop:
	for {
		select {
		case res := <-callback:
			nodes := node.FromNodeInfos(res.NodeInfos)
			for _, n := range nodes {
				idSet[n.ID()] = n
			}

			done++
			if done == l {
				break Loop
			}

		case <-time.After(time.Second * 60):
			// we expected nodes to return results within a reasonable time frame
			break Loop
		}
	}

	// add unique node ids that are closer to target id than closest node
	res := []node.RemoteNodeData{}

	targetDhtID := dht.NewIDFromBase58String(targetID)
	for _, n := range idSet {
		if n.DhtID().Closer(targetDhtID, closestNode.DhtID()) {
			res = append(res, n)
		}
	}

	// sort results by distance from target dht id
	return node.SortClosestPeers(res, targetDhtID)
}

// helper method - a sync wrapper over routingTable.NearestPeers
func (s *swarmImpl) getNearestPeers(dhtID dht.ID, count int) []node.RemoteNodeData {
	psoc := make(table.PeersOpChannel, 1)
	s.routingTable.NearestPeers(table.NearestPeersReq{ID: dhtID, Count: count, Callback: psoc})
	select {
	case c := <-psoc:
		return c.Peers
	}
}
