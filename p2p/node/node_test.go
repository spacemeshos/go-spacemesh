package node

import (
	"testing"
)

func TestUnion(t *testing.T) {
	nodes := GenerateRandomNodesData(10)
	nodes2 := GenerateRandomNodesData(10)
	nodes2 = append(nodes2, nodes[0])

	union := Union(nodes, nodes2)

	if len(union) != 20 {
		t.Fail()
	}
	i := 0
	for n := range nodes {
		for u := range union {
			if nodes[n].String() == union[u].String() {
				union = append(union[:u], union[u+1:]...)
				i++
				break
			}
		}
	}

	for n := range nodes2 {
		for u := range union {
			if nodes2[n].String() == union[u].String() {
				union = append(union[:u], union[u+1:]...)
				i++
				break
			}
		}
	}

	if i != 20 {
		t.Fail()
	}
}

func TestSortByDhtID(t *testing.T) {
	target := GenerateRandomNodeData()
	nodes := GenerateRandomNodesData(10)

	sorted := SortByDhtID(nodes, target.DhtID())
	first := sorted[0]
	for i := 1; i < len(sorted); i++ {
		if target.DhtID().Closer(sorted[i].DhtID(), first.DhtID()) {
			t.Fail()
		}
	}
}
