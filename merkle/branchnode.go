package merkle

import (
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// Am immutable branch (full) node
type branchNode interface {
	GetValue() []byte
	GetPath(entry byte) []byte
	Marshal() pb.Node
}

func newBranchNode(data *pb.Node) branchNode {
	n := &branchNodeImpl{
		entries: make(map[byte][]byte),
		value:   data.Value,
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
	entries map[byte][]byte
	value   []byte
}

func (b *branchNodeImpl) GetValue() []byte { return b.value }
func (b *branchNodeImpl) GetPath(idx byte) []byte {
	return b.entries[idx]
}

func (b *branchNodeImpl) Marshal() pb.Node {

	entries := make([][]byte, 16)

	for idx, val := range b.entries {
		entries[idx] = val
	}

	res := pb.Node{
		NodeType: pb.NodeType_branch,
		Value:    b.value,
		Entries:  entries,
	}

	return res
}
