package dht

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnion(t *testing.T) {
	nodes := generateDiscNodes(10)
	nodes2 := generateDiscNodes(10)
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
	target := generateDiscNode()
	nodes := generateDiscNodes(10)

	sorted := SortByDhtID(nodes, target.DhtID())
	first := sorted[0]
	for i := 1; i < len(sorted); i++ {
		if target.DhtID().Closer(sorted[i].DhtID(), first.DhtID()) {
			t.Fail()
		}
	}
}

func Test_filterNodes(t *testing.T) {
	num := 100
	filternum := 10
	//todo: tabletest?

	list := generateDiscNodes(num)

	filter := make([]discNode, filternum)
	for i := 0; i < filternum; i++ {
		if i == len(list) {
			break
		}
		filter[i] = list[i]
	}

	last := filterNodes(list, filter)

	check := len(list) - filternum

	if check < 0 {
		check = 0
	}

	require.Equal(t, len(last), check)

	for i := 0; i < len(last); i++ {
		for f := 0; f < len(filter); f++ {
			if filter[f] == last[i] {
				t.Fatal("it was there")
			}
		}
	}
}
