package p2p

import (
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"github.com/UnrulyOS/go-unruly/p2p/dht/table"
	"github.com/UnrulyOS/go-unruly/p2p/node"
	"time"
)

// Find a node based on its id - internal method
// id: base58 node id
// returns remote node or nil when not found
// not go safe - should be called from swarm event dispatcher
func (s *swarmImpl) findNode(id string, callback chan node.RemoteNodeData) {

	// look at peer store
	n := s.peers[id]
	if n != nil {
		go func() { callback <- s.localNode.GetRemoteNodeData() }()
		return
	}

	// look at local dht table
	poc := make(table.PeerOpChannel, 1)
	s.routingTable.Find(table.PeerByIdRequest{dht.NewIdFromBase58String(id), poc})
	select {
	case c := <-poc:
		res := c.Peer
		if res != nil {
			go func() { callback <- res }()
			return
		}
	}

	// used kad to locate the node
	s.kadFindNode(id, callback)
}

// Implements the kad algo for locating a remote node
// Precondition - node is not in local routing table
// id - base58 node id string
// Returns requested node or nil if not found
func (s *swarmImpl) kadFindNode(nodeId string, callback chan node.RemoteNodeData) {

	// kad node location algo
	alpha := int(s.config.RoutingTableAlpha)
	k := int(s.config.RoutingTableBucketSize)
	dhtId := dht.NewIdFromBase58String(nodeId)

	// step 1 - get up to alpha closest nodes to the target in the local routing table
	searchList := s.getNearestPeers(dhtId, k)

	// step 2 - iterative lookups for nodeId using searchList

Loop:
	for {

		if searchList == nil || len(searchList) == 0 {
			go func() { callback <- nil }()
			break Loop
		}

		closestNode := searchList[0]

		if closestNode.Id() == nodeId {
			go func() { callback <- closestNode }()
			break Loop
		}

		// pick up to alpha server to query from the search list
		// servers that have been recently queried will not be returend
		servers := node.PickFindNodeServers(searchList, nodeId, alpha)

		if len(servers) == 0 {
			// no more server to query
			go func() { callback <- nil }()
			break Loop
		}

		// lookup nodeId using the target servers
		res := s.lookupNode(servers, nodeId, closestNode)

		if len(res) >= 0 {

			// merge newly found nodes
			searchList = node.Union(searchList, res)

			// sort by distance from target
			searchList = node.SortClosestPeers(res, dhtId)
		}

		// keep iterating using new servers that were not queried yet from searchlist (if any)
	}
}

// Lookup a target node on one or more servers
// Returns closest nodes which are closers than closestNode to targetId
// If node found it will be in top of results list
func (s *swarmImpl) lookupNode(servers []node.RemoteNodeData, targetId string, closestNode node.RemoteNodeData) []node.RemoteNodeData {

	l := len(servers)

	if l == 0 {
		return []node.RemoteNodeData{}
	}

	// results channel
	callback := make(chan FindNodeResp, l)

	// queries are run in par and results are collected
	for i := 0; i < l; i++ {
		servers[i].SetLastFindNodeCall(targetId, time.Now())

		// find node protocol adds found nodes to the local routing table
		go s.getFindNodeProtocol().FindNode(crypto.UUID(), servers[i].Id(), targetId, callback)
	}

	done := 0
	idSet := make(map[string]node.RemoteNodeData)

Loop:
	for {
		select {
		case res := <-callback:
			nodes := node.FromNodeInfos(res.NodeInfos)
			for _, n := range nodes {
				idSet[n.Id()] = n
			}

			done += 1
			if done == l {
				break Loop
			}
		}
	}

	// add unique node ids that are closer to target id than closest node
	res := []node.RemoteNodeData{}

	targetDhtId := dht.NewIdFromBase58String(targetId)
	for _, n := range idSet {
		if n.DhtId().Closer(targetDhtId, closestNode.DhtId()) {
			res = append(res, n)
		}
	}

	// sort results by distance from target dht id
	res = node.SortClosestPeers(res, targetDhtId)

	return res
}

// helper method - sync wrapper to routingTable.NearestPeers
func (s *swarmImpl) getNearestPeers(dhtId dht.ID, count int) []node.RemoteNodeData {
	psoc := make(table.PeersOpChannel, 1)
	s.routingTable.NearestPeers(table.NearestPeersReq{dhtId, count, psoc})
	select {
	case c := <-psoc:
		return c.Peers
	}
}
