package dht

import "github.com/spacemeshos/go-spacemesh/p2p/node"

type discNode struct {
	node.Node
	udpAddress string
}

var emptyDiscNode = discNode{node.EmptyNode, ""}

// SortByDhtID Sorts a Node array by DhtID id, returns a sorted array
func SortByDhtID(nodes []discNode, id node.DhtID) []discNode {
	for i := 1; i < len(nodes); i++ {
		v := nodes[i]
		j := i - 1
		for j >= 0 && id.Closer(v.DhtID(), nodes[j].DhtID()) {
			nodes[j+1] = nodes[j]
			j = j - 1
		}
		nodes[j+1] = v
	}
	return nodes
}

// Union returns a union of 2 lists of nodes.
func Union(list1 []discNode, list2 []discNode) []discNode {

	idSet := map[string]discNode{}

	for _, n := range list1 {
		idSet[n.String()] = n
	}
	for _, n := range list2 {
		if _, ok := idSet[n.String()]; !ok {
			idSet[n.String()] = n
		}
	}

	res := make([]discNode, len(idSet))
	i := 0
	for _, n := range idSet {
		res[i] = n
		i++
	}

	return res
}
