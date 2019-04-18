package dht

import (
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"testing"
)

// Tests basic bucket features
func TestBucket(t *testing.T) {
	const n = 100

	local := generateDiscNode()

	localID := local.DhtID()

	// add 100 nodes to the table
	b := NewBucket()
	nodes := generateDiscNodes(n)

	for i := 0; i < n; i++ {
		b.PushFront(nodes[i])
	}

	// find a random identity
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
	assert.True(t, removed, "expected identity to be removed")

	if b.Has(nodes[i]) {
		t.Errorf("expected identity to be removed: %v", nodes[i])
	}

	// test split
	newBucket := b.Split(0, localID)
	items := b.List()

	for e := items.Front(); e != nil; e = e.Next() {
		id := e.Value.(discNode).DhtID()
		cpl := id.CommonPrefixLen(localID)
		if cpl > 0 {
			t.Fatalf("Split failed. found id with cpl > 0 in bucket. Should all be with cpl of 0")
		}
	}

	items = newBucket.List()
	for e := items.Front(); e != nil; e = e.Next() {
		id := e.Value.(discNode).DhtID()
		cpl := id.CommonPrefixLen(localID)
		if cpl == 0 {
			t.Fatalf("Split failed. found id with cpl == 0 in non 0 bucket, should all be with cpl > 0")
		}
	}
}
