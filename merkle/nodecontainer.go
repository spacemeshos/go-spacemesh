package merkle

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/syndtr/goleveldb/leveldb"
)

type NodeContainer interface {
	getNodeType() pb.NodeType
	getLeafNode() shortNode
	getExtNode() shortNode
	getBranchNode() branchNode
	marshal() ([]byte, error) // get binary encoded marshaled node data
	getNodeHash() []byte
	loadedChildren() bool              // returns true iff child nodes loaded to memory
	loadChildren(db *leveldb.DB) error // load all children from db
}

type nodeContainerImp struct {
	nodeType       pb.NodeType // contained node type
	leaf           shortNode   // lead node data or nil
	branch         branchNode  // branch node data or nil
	ext            shortNode   // ext node data or nil
	childrenLoaded bool        // indicates if children loaded from db

	children map[string]NodeContainer // k -pointer to child node (hex encoded). v- child
}

func (n *nodeContainerImp) getNodeType() pb.NodeType {
	return n.nodeType
}

func (n *nodeContainerImp) getLeafNode() shortNode {
	return n.leaf
}

func (n *nodeContainerImp) getExtNode() shortNode {
	return n.ext
}

func (n *nodeContainerImp) getBranchNode() branchNode {
	return n.branch
}

func (n *nodeContainerImp) loadedChildren() bool {
	return n.childrenLoaded
}

// loads all node childrens to memory from the db
// calling this on a root will load the entire tree to memory

func (n *nodeContainerImp) loadChildren(db *leveldb.DB) error {

	// mark this node as child loaded when done
	defer func() { n.childrenLoaded = true }()

	if n.nodeType == pb.NodeType_leaf {
		// leaves are childless
		return nil
	}

	if n.nodeType == pb.NodeType_extension {

		// value in an extension node is a pointer to child - load it
		key := n.ext.getValue()
		data, err := db.Get(key, nil)
		if err != nil {
			return err
		}

		node, err := newNodeFromData(data)
		if err != nil {
			return err
		}

		n.children[hex.EncodeToString(key)] = node
		return node.loadChildren(db)
	}

	if n.nodeType == pb.NodeType_branch {

		keys := n.branch.getAllChildNodePointers()
		for _, key := range keys {
			data, err := db.Get(key, nil)
			if err != nil {
				return err
			}
			node, err := newNodeFromData(data)
			if err != nil {
				return err
			}
			n.children[hex.EncodeToString(key)] = node
		}

	}

	return nil
}

func (n *nodeContainerImp) getNodeHash() []byte {
	switch n.nodeType {
	case pb.NodeType_branch:
		return n.branch.getNodeHash()
	case pb.NodeType_leaf:
		return n.leaf.getNodeHash()
	case pb.NodeType_extension:
		return n.ext.getNodeHash()
	default:
		return nil
	}
}

func (n *nodeContainerImp) marshal() ([]byte, error) {
	switch n.nodeType {
	case pb.NodeType_branch:
		return n.branch.marshal()
	case pb.NodeType_leaf:
		return n.leaf.marshal()
	case pb.NodeType_extension:
		return n.ext.marshal()
	default:
		return nil, errors.New(fmt.Sprintf("unexpcted node type %d", n.nodeType))
	}
}

func newNodeFromData(data []byte) (NodeContainer, error) {

	n := &pb.Node{}
	err := proto.Unmarshal(data, n)
	if err != nil {
		return nil, err
	}

	c := &nodeContainerImp{
		children: make(map[string]NodeContainer),
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
