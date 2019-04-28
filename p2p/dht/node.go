package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"sort"
)

type discNode struct {
	node.Node
	udpAddress string
}

var emptyDiscNode = discNode{node.EmptyNode, ""}

type byDhtID struct {
	arr    []discNode
	target node.DhtID
}

func (s byDhtID) Len() int {
	return len(s.arr)
}
func (s byDhtID) Swap(i, j int) {
	s.arr[i], s.arr[j] = s.arr[j], s.arr[i]
}
func (s byDhtID) Less(i, j int) bool {
	return s.target.Closer(s.arr[i].DhtID(), s.arr[j].DhtID())
}

func SortByDhtID(nodes []discNode, target node.DhtID) []discNode {
	by := byDhtID{nodes, target}
	sort.Sort(by)
	return by.arr
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

func filterNodes(list []discNode, filter []discNode) []discNode {
	newlist := make([]discNode, 0, len(list))
loop:
	for i := 0; i < len(list); i++ {
		for j := 0; j < len(filter); j++ {
			if filter[j] == list[i] {
				continue loop
			}
		}
		newlist = append(newlist, list[i])
	}
	return newlist
}
