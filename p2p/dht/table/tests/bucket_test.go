package tests

import (
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
)

// Tests basic bucket features
func TestBucket(t *testing.T) {
	const n = 100

	local, err := p2p.GenerateRandomNodeData()
	assert.NoError(t, err, "Should be able to create node")

	localID := local.DhtID()

	// add 100 nodes to the table
	b := table.NewBucket()
	nodes, err := p2p.GenerateRandomNodesData(n)
	assert.NoError(t, err, "Should be able to create multiple nodes")

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
	removed := b.Remove(nodes[i])
	assert.True(t, removed, "expected node to be removed")

	if b.Has(nodes[i]) {
		t.Errorf("expected node to be removed: %v", nodes[i])
	}

	// test split
	newBucket := b.Split(0, localID)
	items := b.List()

	for e := items.Front(); e != nil; e = e.Next() {
		id := e.Value.(node.RemoteNodeData).DhtID()
		cpl := id.CommonPrefixLen(localID)
		if cpl > 0 {
			t.Fatalf("Split failed. found id with cpl > 0 in bucket. Should all be with cpl of 0")
		}
	}

	items = newBucket.List()
	for e := items.Front(); e != nil; e = e.Next() {
		id := e.Value.(node.RemoteNodeData).DhtID()
		cpl := id.CommonPrefixLen(localID)
		if cpl == 0 {
			t.Fatalf("Split failed. found id with cpl == 0 in non 0 bucket, should all be with cpl > 0")
		}
	}
}
