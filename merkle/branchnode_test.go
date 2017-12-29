package merkle

import (
	"bytes"
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"testing"
)

func TestBranchNodeCreation(t *testing.T) {

	v1 := []byte("fake node 1")
	p1 := crypto.Sha256(v1)
	p1Hex := hex.EncodeToString(p1)
	p1Val, err := charToHex(string(p1Hex[0]))
	assert.NoErr(t, err, "failed to get first hex char")
	assert.Equal(t, p1Val, byte(8), "unexpected value")

	v2 := []byte("fake node 2")
	p2 := crypto.Sha256(v2)
	p2Hex := hex.EncodeToString(p2)
	p2Val, err := charToHex(string(p2Hex[0]))
	assert.NoErr(t, err, "failed to get first hex char")
	assert.Equal(t, p2Val, byte(2), "unexpected value")

	v3 := []byte("some user data") // user data
	k3 := crypto.Sha256(v3)        // value stored at node

	entries := make(map[byte][]byte)
	entries[p1Val] = p1
	entries[p2Val] = p2

	node := newBranchNode(entries, k3)

	value := node.getValue()
	assert.True(t, bytes.Equal(k3, value), "unexpected node value")

	nodeHash := node.getNodeHash()
	binData, err := node.marshal()
	assert.NoErr(t, err, "failed to marshal node")
	assert.True(t, bytes.Equal(crypto.Sha256(binData), nodeHash), "unexpected node hash")

	res := node.getPath(p1Val)
	assert.NotNil(t, res, "expected to get a slice")
	assert.True(t, len(res) > 0, "expected a non-empty slice")
	assert.True(t, bytes.Equal(res, p1), "unexpected pointer value")

	res = node.getPath(p2Val)
	assert.NotNil(t, res, "expected to get a slice")
	assert.True(t, len(res) > 0, "expected a non-empty slice")
	assert.True(t, bytes.Equal(res, p2), "unexpected pointer value")

	idx, err := charToHex("a")
	assert.NoErr(t, err, "failed to get hex value")
	res = node.getPath(idx)
	assert.NotNil(t, res, "expected []byte slice")
	assert.True(t, len(res) == 0, "expected empty []byte slice")

	pointers := node.getAllChildNodePointers()

	assert.NotNil(t, pointers, "expected []byte slice")
	assert.True(t, len(pointers) == 2, "expected 2 stored pointers")

}
