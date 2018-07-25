package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	_, pu, _ := crypto.GenerateKeyPair()
	address := "0.0.0.0:1234"

	node := New(pu, address)
	fmt.Print(pu.String())

	assert.Equal(t, node.PublicKey(), pu)
	assert.Equal(t, node.Address(), address)
}

func TestNewNodeFromString(t *testing.T) {
	address := "126.0.0.1:3572"
	pubkey := "r9gJRWVB9JVPap2HKnduoFySvHtVTfJdQ4WG8DriUD82"
	data := fmt.Sprintf("%v/%v", address, pubkey)

	node, err := NewNodeFromString(data)

	assert.NoError(t, err)
	assert.Equal(t, node.Address(), address)
	assert.Equal(t, node.PublicKey().String(), pubkey)

	pubkey = "r9gJRWVB9JVPap2HKn"
	data = fmt.Sprintf("%v/%v", address, pubkey)
	node, err = NewNodeFromString(data)
	assert.Error(t, err)
}

func TestStringFromNode(t *testing.T) {
	n := GenerateRandomNodeData()

	str := StringFromNode(n)
	splt := strings.Split(str, "/")

	assert.Equal(t, splt[0], n.address)
	assert.Equal(t, splt[1], n.PublicKey().String())
}

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
