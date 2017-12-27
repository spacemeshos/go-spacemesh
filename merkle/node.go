package merkle

import (
	"encoding/hex"
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// shortNode is a leaf or an extension node
type shortNode interface {
	IsLeaf() bool
	GetValue() []byte
	GetPath() string // hex encoded string
	GetParity() bool
}

type shortNodeImpl struct {
	isLeaf bool // extension node when false
	parity bool // path parity - when odd, truncate first nibble prefix to return path
	path   []byte
	value  []byte
}

func (l *shortNodeImpl) GetValue() []byte { return l.value }
func (l *shortNodeImpl) GetPath() string  { return hex.EncodeToString(l.path) }
func (l *shortNodeImpl) GetParity() bool  { return l.parity }
func (l *shortNodeImpl) IsLeaf() bool     { return l.isLeaf }

func newShortNode(data *pb.Node) shortNode {

	n := &shortNodeImpl{
		isLeaf: data.NodeType == pb.NodeType_leaf,
		parity: data.Parity,
		path:   data.Path,
		value:  data.Value,
	}
	return n
}

// A branch (full) node
type branchNode interface {
	GetValue() []byte
	GetPath(entry byte) []byte
}

type branchNodeImpl struct {
	entries map[byte][]byte
	value   []byte
}

func (b *branchNodeImpl) GetValue() []byte { return b.value }
func (b *branchNodeImpl) GetPath(idx byte) []byte {
	return b.entries[idx]
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

type NodeContainer interface {
	GetNodeType() pb.NodeType
	GetLeafNode() shortNode
	GetExtNode() shortNode
	GetBranchNode() branchNode
}

type nodeContainerImp struct {
	nodeType pb.NodeType
	leaf     shortNode
	branch   branchNode
	ext      shortNode
}

func (n *nodeContainerImp) GetNodeType() pb.NodeType {
	return n.nodeType
}

func (n *nodeContainerImp) GetLeafNode() shortNode {
	return n.leaf
}

func (n *nodeContainerImp) GetExtNode() shortNode {
	return n.ext
}

func (n *nodeContainerImp) GetBranchNode() branchNode {
	return n.branch
}

func NodeFromData(data []byte) (NodeContainer, error) {

	n := &pb.Node{}
	err := proto.Unmarshal(data, n)
	if err != nil {
		return nil, err
	}

	c := &nodeContainerImp{}

	switch n.NodeType {
	case pb.NodeType_branch:
		c.nodeType = pb.NodeType_branch
		c.branch = newBranchNode(n)
	case pb.NodeType_extension:
		c.nodeType = pb.NodeType_extension
		c.ext = newShortNode(n)

	case pb.NodeType_leaf:
		c.nodeType = pb.NodeType_leaf
		c.leaf = newShortNode(n)

	default:
		return nil, errors.New("unexpected node type")
	}

	return c, nil
}
