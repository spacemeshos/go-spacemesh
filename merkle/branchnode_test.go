package merkle

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/stretchr/testify/assert"
)

func TestBranchNodeCreation(t *testing.T) {

	v1 := []byte("fake node 1")
	p1 := crypto.Sha256(v1)
	p1Hex := hex.EncodeToString(p1)

	v2 := []byte("fake node 2")
	p2 := crypto.Sha256(v2)
	p2Hex := hex.EncodeToString(p2)

	v3 := []byte("some user data") // user data
	k3 := crypto.Sha256(v3)        // value stored at node

	entries := make(map[string][]byte)
	entries[p1Hex] = p1
	entries[p2Hex] = p2

	node := newBranchNodeEx(entries, k3)

	value := node.getValue()
	assert.True(t, bytes.Equal(k3, value), "unexpected node value")

	nodeHash := node.getNodeHash()
	binData, err := node.marshal()
	assert.NoError(t, err, "failed to marshal node")
	assert.True(t, bytes.Equal(crypto.Sha256(binData), nodeHash), "unexpected node hash")

	res := node.getPointer(string(p1Hex[0]))
	assert.NotNil(t, res, "expected to get a slice")
	assert.True(t, len(res) > 0, "expected a non-empty slice")
	assert.True(t, bytes.Equal(res, p1), "unexpected pointer value")

	res = node.getPointer(string(p2Hex[0]))
	assert.NotNil(t, res, "expected to get a slice")
	assert.True(t, len(res) > 0, "expected a non-empty slice")
	assert.True(t, bytes.Equal(res, p2), "unexpected pointer value")

	res = node.getPointer("a")
	assert.Equal(t, res, []byte(nil))
	assert.True(t, len(res) == 0, "expected empty []byte slice")

	pointers := node.getAllChildNodePointers()

	assert.NotNil(t, pointers, "expected []byte slice")
	assert.True(t, len(pointers) == 2, "expected 2 stored pointers")

}
