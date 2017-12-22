package p2p

import (
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"github.com/UnrulyOS/go-unruly/p2p/dht/table"
	"github.com/UnrulyOS/go-unruly/p2p/node"
)

// Find a node based on its id - internal method
// id: base58 encoded node id
// returns remote node data or nil if find fails
func (s *swarmImpl) findNode(id string, callback chan node.RemoteNodeData) {

	// special case - local node
	if s.localNode.String() == id {
		go func() { callback <- s.localNode.GetRemoteNodeData() }()
		return
	}

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
func (s *swarmImpl) kadFindNode(id string, callback chan node.RemoteNodeData) {

	// dht algo for locating a node
	const alpha = 3
	dhtId := dht.NewIdFromBase58String(id)

	// step 1 - get up to alpha closest nodes to the target in the local routing table
	searchList := s.getNearestPeers(dhtId, alpha)
	if searchList == nil || len(searchList) == 0 {
		go func() { callback <- nil }()
		return
	}

	//closestNode := searchList[0]

	// pick alpha nodes from the list

	// query the picked nodes in parallel for nearest nodes

	// closestNode is picked from the top of each results based on previous closedNode

	// when queering a node - update last nearest query time

	// stop if node found, otherwise if alpha queries returned closer nodes then what we know about - query these nodes

	// update the search list with the results from queries
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

// helper - sync findNodeReq
func (s *swarmImpl) findNodeReq(serverId string, nodeId string) []node.RemoteNodeData {
	reqId := crypto.UUID()

	fcallback := make(chan FindNodeResp, 1)
	s.getFindNodeProtocol().FindNode(reqId, serverId, nodeId, fcallback)

	/*
		select {
			c := <- fcallback:
				if !bytes.Equal(c.reqId, reqId) {
					continue
				}

		}*/
	return nil
}
