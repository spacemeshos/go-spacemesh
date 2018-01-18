package tests

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"math/rand"
	"testing"
)

// Tests basic bucket features
func TestBucket(t *testing.T) {
	const n = 100

	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()

	// add 100 nodes to the table
	b := table.NewBucket()
	nodes := p2p.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		b.PushFront(nodes[i])
	}

	// find a random node
	i := rand.Intn(len(nodes))
	if !b.Has(nodes[i]) {
		t.Errorf("Failed to find peer: %v", nodes[i])
	}

	// find all nodes
	for i := 0; i < n; i++ {
		if !b.Has(nodes[i]) {
			t.Errorf("Failed to find peer: %v", nodes[i])
		}
	}

	// remove random nodes
	i = rand.Intn(len(nodes))
	b.Remove(nodes[i])
	if b.Has(nodes[i]) {
		t.Errorf("expected node to be removed: %v", nodes[i])
	}

	// test split
	newBucket := b.Split(0, localId)
	items := b.List()

	for e := items.Front(); e != nil; e = e.Next() {
		id := e.Value.(node.RemoteNodeData).DhtId()
		cpl := id.CommonPrefixLen(localId)
		if cpl > 0 {
			t.Fatalf("Split failed. found id with cpl > 0 in bucket. Should all be with cpl of 0")
		}
	}

	items = newBucket.List()
	for e := items.Front(); e != nil; e = e.Next() {
		id := e.Value.(node.RemoteNodeData).DhtId()
		cpl := id.CommonPrefixLen(localId)
		if cpl == 0 {
			t.Fatalf("Split failed. found id with cpl == 0 in non 0 bucket, should all be with cpl > 0")
		}
	}
}
