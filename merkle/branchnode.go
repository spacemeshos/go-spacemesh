package merkle

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// An immutable branch (full) node
type branchNode interface {
	getValue() []byte          // value terminated in this path or nil
	getPath(entry byte) []byte // return pointer to child node for hex char entry or nil
	marshal() ([]byte, error)  // to binary data
	getNodeHash() []byte       // data hash (pointer to this node)

	getAllChildNodePointers() [][]byte // get all pointers to child nodes
}

// creates a new branchNode from provided data
func newBranchNode(entries map[byte][]byte, value []byte) (branchNode, error) {

	node := &branchNodeImpl{
		value:   value,
		entries: entries,
	}

	// Marshal the data to generate the node's hash
	d, err := node.marshal()
	if err != nil {
		return nil, err
	}
	node.nodeHash = crypto.Sha256(d)
	return node, nil
}

// Createss a new branch node from persisted branch node data
func newBranchNodeFromPersistedData(rawData []byte, data *pb.Node) branchNode {

	n := &branchNodeImpl{
		nodeHash: crypto.Sha256(rawData),
		entries:  make(map[byte][]byte),
		value:    data.Value,
	}

	// populate entries table
	for idx, val := range data.Entries {
		if len(val) > 0 {
			n.entries[byte(idx)] = val
		}
	}

	return n
}

type branchNodeImpl struct {
	entries  map[byte][]byte
	value    []byte
	nodeHash []byte
}

func (b *branchNodeImpl) getAllChildNodePointers() [][]byte {
	res := make([][]byte, 0)
	for _, val := range b.entries {
		if len(val) > 0 {
			res = append(res, val)
		}
	}
	return res
}

func (b *branchNodeImpl) getNodeHash() []byte {
	return b.nodeHash
}

func (b *branchNodeImpl) getValue() []byte { return b.value }
func (b *branchNodeImpl) getPath(idx byte) []byte {
	return b.entries[idx]
}

func (b *branchNodeImpl) marshal() ([]byte, error) {

	entries := make([][]byte, 16)
	for idx, val := range b.entries {
		entries[idx] = val
	}

	res := &pb.Node{
		NodeType: pb.NodeType_branch,
		Value:    b.value,
		Entries:  entries,
	}

	return proto.Marshal(res)
}
