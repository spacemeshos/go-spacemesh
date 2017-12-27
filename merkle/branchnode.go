package merkle

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// An immutable branch (full) node
type branchNode interface {
	GetValue() []byte
	GetPath(entry byte) []byte
	Marshal() ([]byte, error)
	GetNodeHash() []byte
}

// creates a new branchNode from provided data
func newBranchNode(entries map[byte][]byte, value []byte) (branchNode, error) {

	node := &branchNodeImpl{
		value:    value,
		entries: entries,
	}

	d, err := node.Marshal()
	if err != nil {
		return nil, err
	}

	node.nodeHash = crypto.Sha256(d)
	return node, nil
}

// creates a new branch node from persisted branch node
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

func (b *branchNodeImpl) GetNodeHash() []byte {
	return b.nodeHash
}

func (b *branchNodeImpl) GetValue() []byte { return b.value }
func (b *branchNodeImpl) GetPath(idx byte) []byte {
	return b.entries[idx]
}

func (b *branchNodeImpl) Marshal() ([]byte, error) {

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
