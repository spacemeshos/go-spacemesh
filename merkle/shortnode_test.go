package merkle

import (
	"bytes"
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLeafNode(t *testing.T) {

	v1 := []byte("fake node 1")
	p1 := crypto.Sha256(v1)
	p1Hex := hex.EncodeToString(p1)

	node := newShortNode(pb.NodeType_leaf, p1Hex, p1)

	p := node.getPath()
	assert.Equal(t, p1Hex, p, "expected same path")

	assert.True(t, node.isLeaf(), "expected leaf")

	v := node.getValue()
	assert.True(t, bytes.Equal(v, p1), "unexpected value")

	nodeHash := node.getNodeHash()
	binData, err := node.marshal()
	assert.NoError(t, err, "failed to marshal node")
	assert.True(t, bytes.Equal(crypto.Sha256(binData), nodeHash), "unexpected node hash")

}

func TestExtNode(t *testing.T) {

	v1 := []byte("fake node 1")
	p1 := crypto.Sha256(v1)
	p1Hex := hex.EncodeToString(p1)

	node := newShortNode(pb.NodeType_extension, p1Hex, p1)

	p := node.getPath()
	assert.Equal(t, p1Hex, p, "expected same path")

	assert.False(t, node.isLeaf(), "expected leaf")

	v := node.getValue()
	assert.True(t, bytes.Equal(v, p1), "unexpected value")

	nodeHash := node.getNodeHash()
	binData, err := node.marshal()
	assert.NoError(t, err, "failed to marshal node")
	assert.True(t, bytes.Equal(crypto.Sha256(binData), nodeHash), "unexpected node hash")
}
