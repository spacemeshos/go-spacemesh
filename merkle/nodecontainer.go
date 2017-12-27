package merkle

import (
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

type NodeContainer interface {
	GetNodeType() pb.NodeType
	GetLeafNode() shortNode
	GetExtNode() shortNode
	GetBranchNode() branchNode
	Marshal() ([]byte, error) // get binary encoded marshaled node data
	GetNodeHash() []byte
	LoadedChildren() bool	// returns true iff child nodes loaded to memory
	LoadChildren(/* tree-db-here*/) error // load all children from db
}

type nodeContainerImp struct {
	nodeType pb.NodeType
	leaf     shortNode
	branch   branchNode
	ext      shortNode
	loadedChildren bool

	children map[byte]NodeContainer // k -pointer to child node. v- child
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

func (n *nodeContainerImp) LoadedChildren() bool {
	return n.loadedChildren
}

// loads all node childrens to memory from the db
// calling this on a root will load the entire tree to memory

func (n *nodeContainerImp) LoadChildren(/*db*/) error {

	// mark this node as child loaded when done
	defer func() { n.loadedChildren = true }()

	if n.nodeType == pb.NodeType_leaf {
		// lead has no children
		return nil
	}

	if n.nodeType == pb.NodeType_extension {

		// value in an extension node is a pointer to child

		// key := n.ext.GetValue()

		// todo: load child node from db by this key

		// call LoadChildren(/*db*/) on that node

		// todo: save child in childs map

	}

	if n.nodeType == pb.NodeType_branch {

		// keys := n.branch.GetAllChildNodePointers()

		// todo: load all childs from db here

		//  call LoadChildren(/*db*/) on each node

		// save each loaded child in childs map

	}

	return nil
}

func (n *nodeContainerImp) GetNodeHash() []byte {
	switch n.nodeType {
	case pb.NodeType_branch:
		return n.branch.GetNodeHash()
	case pb.NodeType_leaf:
		return n.leaf.GetNodeHash()
	case pb.NodeType_extension:
		return n.ext.GetNodeHash()
	default:
		return nil
	}
}

func (n *nodeContainerImp) Marshal() ([]byte, error) {
	switch n.nodeType {
	case pb.NodeType_branch:
		return n.branch.Marshal()
	case pb.NodeType_leaf:
		return n.leaf.Marshal()
	case pb.NodeType_extension:
		return n.ext.Marshal()
	default:
		return nil, errors.New(fmt.Sprintf("unexpcted node type %d", n.nodeType))
	}
}

func NodeFromData(data []byte) (NodeContainer, error) {

	n := &pb.Node{}
	err := proto.Unmarshal(data, n)
	if err != nil {
		return nil, err
	}

	c := &nodeContainerImp{
		children: make(map[byte]NodeContainer),
	}

	switch n.NodeType {
	case pb.NodeType_branch:
		c.nodeType = pb.NodeType_branch
		c.branch = newBranchNodeFromPersistedData(data, n)
	case pb.NodeType_extension:
		c.nodeType = pb.NodeType_extension
		c.ext = newShortNodeFromData(data, n)
	case pb.NodeType_leaf:
		c.nodeType = pb.NodeType_leaf
		c.leaf = newShortNodeFromData(data, n)
	default:
		return nil, errors.New("unexpected node type")
	}

	return c, nil
}
