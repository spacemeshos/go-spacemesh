package merkle

import (
	"bytes"
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"testing"
)

func TestBranchNodeContainer(t *testing.T) {

	// create a branch node
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

	b, err := newBranchNode(entries, k3)
	assert.NoErr(t, err, "failed to create branch node")

	// branch node container
	bData, err := b.marshal()
	assert.NoErr(t, err, "failed to marshal branch node")

	node, err := newNodeFromData(bData)
	assert.NoErr(t, err, "failed to create node container from branch node")

	assert.True(t, node.getNodeType() == pb.NodeType_branch, "expected branch")
	bn := node.getBranchNode()
	assert.NotNil(t, bn, "expected branch node")

	assert.True(t, bytes.Equal(bn.getNodeHash(), b.getNodeHash()), "hash mismatch")

	assert.Nil(t, node.getExtNode(), "expected branch node")
	assert.Nil(t, node.getLeafNode(), "expected branch node")

	data, err := node.marshal()
	assert.NoErr(t, err, "failed to marshal branch node")

	hash := node.getNodeHash()
	assert.True(t, bytes.Equal(crypto.Sha256(data), hash), "hash mismatch")

	node1, err := newBranchNodeContainer(entries, k3)
	assert.NoErr(t, err, "failed to create branch node")

	assert.True(t, node1.getNodeType() == pb.NodeType_branch, "expected branch")
	bn = node.getBranchNode()
	assert.NotNil(t, bn, "expected branch node")
	assert.True(t, bytes.Equal(bn.getNodeHash(), b.getNodeHash()), "hash mismatch")

	assert.Nil(t, node1.getExtNode(), "expected branch node")
	assert.Nil(t, node1.getLeafNode(), "expected branch node")

	data, err = node1.marshal()
	assert.NoErr(t, err, "failed to marshal branch node")

	hash = node1.getNodeHash()
	assert.True(t, bytes.Equal(crypto.Sha256(data), hash), "hash mismatch")

}

func TestLeafNodeContainer(t *testing.T) {

	// leaf node
	l := []byte("fake node 1")
	l1 := crypto.Sha256(l)
	l1Hex := hex.EncodeToString(l1)

	leaf, err := newShortNode(pb.NodeType_leaf, l1Hex, l1)
	assert.NoErr(t, err, "failed to create leaf node")

	data, err := leaf.marshal()
	assert.NoErr(t, err, "failed to marshal leaf node")

	node, err := newNodeFromData(data)
	assert.NoErr(t, err, "failed to create node container from leaf node")

	assert.True(t, node.getNodeType() == pb.NodeType_leaf, "expected leaf")
	ln := node.getLeafNode()
	assert.NotNil(t, ln, "expected leaf node")
	assert.True(t, bytes.Equal(ln.getNodeHash(), leaf.getNodeHash()), "hash mismatch")

	assert.Nil(t, node.getExtNode(), "expected leaf node")
	assert.Nil(t, node.getBranchNode(), "expected leaf node")

	data1, err := node.marshal()
	assert.NoErr(t, err, "failed to marshal leaf node")

	hash := node.getNodeHash()
	assert.True(t, bytes.Equal(crypto.Sha256(data1), hash), "hash mismatch")

	node1, err := newLeftNodeContainer(l1Hex, l1)
	assert.NoErr(t, err, "failed to create node container from leaf node")

	assert.True(t, node1.getNodeType() == pb.NodeType_leaf, "expected leaf")

	ln = node1.getLeafNode()

	assert.NotNil(t, ln, "expected leaf node")
	assert.True(t, bytes.Equal(ln.getNodeHash(), leaf.getNodeHash()), "hash mismatch")

	assert.Nil(t, node1.getExtNode(), "expected leaf node")
	assert.Nil(t, node1.getBranchNode(), "expected leaf node")

	data1, err = node1.marshal()
	assert.NoErr(t, err, "failed to marshal leaf node")

	hash = node1.getNodeHash()
	assert.True(t, bytes.Equal(crypto.Sha256(data1), hash), "hash mismatch")
}

func TestExtNodeContainer(t *testing.T) {

	// ext node
	e := []byte("fake node 1")
	e1 := crypto.Sha256(e)
	e1Hex := hex.EncodeToString(e1)

	ext, err := newShortNode(pb.NodeType_extension, e1Hex, e1)
	assert.NoErr(t, err, "failed to create ext node")

	data, err := ext.marshal()
	assert.NoErr(t, err, "failed to marshal ext node")

	node, err := newNodeFromData(data)
	assert.NoErr(t, err, "failed to create node container from ext node")

	assert.True(t, node.getNodeType() == pb.NodeType_extension, "expected ext")
	en := node.getExtNode()
	assert.NotNil(t, en, "expected ext node")
	assert.True(t, bytes.Equal(en.getNodeHash(), ext.getNodeHash()), "hash mismatch")

	assert.Nil(t, node.getLeafNode(), "expected ext node")
	assert.Nil(t, node.getBranchNode(), "expected ext node")

	data1, err := node.marshal()
	assert.NoErr(t, err, "failed to marshal ext node")

	hash := node.getNodeHash()
	assert.True(t, bytes.Equal(crypto.Sha256(data1), hash), "hash mismatch")


	node1, err := newExtNodeContainer(e1Hex, e1)
	assert.NoErr(t, err, "failed to create node container from ext node")

	assert.True(t, node1.getNodeType() == pb.NodeType_extension, "expected ext")

	en = node1.getExtNode()

	assert.NotNil(t, en, "expected leaf node")
	assert.True(t, bytes.Equal(en.getNodeHash(), ext.getNodeHash()), "hash mismatch")

	assert.Nil(t, node1.getLeafNode(), "expected ext node")
	assert.Nil(t, node1.getBranchNode(), "expected ext node")

	data1, err = node1.marshal()
	assert.NoErr(t, err, "failed to marshal ext node")

	hash = node1.getNodeHash()
	assert.True(t, bytes.Equal(crypto.Sha256(data1), hash), "hash mismatch")

}
