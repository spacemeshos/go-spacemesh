package merkle

import (
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

type NodeContainer interface {
	GetNodeType() pb.NodeType
	GetLeafNode() shortNode
	GetExtNode() shortNode
	GetBranchNode() branchNode

	Marshal() ([]byte, error) // get binary encoded marshaled node data

	GetNodeHash() []byte
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

	c := &nodeContainerImp{}

	switch n.NodeType {
	case pb.NodeType_branch:
		c.nodeType = pb.NodeType_branch
		c.branch = newBranchNodeFromPersistedData(data, n)
	case pb.NodeType_extension:
		c.nodeType = pb.NodeType_extension
		c.ext = newShortNodeFromPersistedData(data, n)
	case pb.NodeType_leaf:
		c.nodeType = pb.NodeType_leaf
		c.leaf = newShortNodeFromPersistedData(data, n)
	default:
		return nil, errors.New("unexpected node type")
	}

	return c, nil
}
