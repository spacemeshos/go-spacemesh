package merkle

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/syndtr/goleveldb/leveldb"
)

var ErrorInvalidHexChar = errors.New("Invalid hex char")

type NodeContainer interface {
	getNodeType() pb.NodeType
	getLeafNode() shortNode
	getExtNode() shortNode
	getBranchNode() branchNode
	marshal() ([]byte, error) // get binary encoded marshaled node data
	getNodeHash() []byte

	loadChildren(db *leveldb.DB) error // load all direct children from store

	getChild(key string) NodeContainer

	addBranchChild(prefix string, child NodeContainer) error
	removeBranchChild(prefix string) error

	getParent() NodeContainer
	setParent(p NodeContainer)
}

type nodeContainerImp struct {
	nodeType pb.NodeType // contained node type
	leaf     shortNode   // lead node data or nil
	branch   branchNode  // branch node data or nil
	ext      shortNode   // ext node data or nil

	parent NodeContainer

	// note that this hold child of branch node or of an extension node
	children map[string]NodeContainer // k -pointer to child node (hex encoded). v- child
}

func newLeftNodeContainer(path string, value []byte, parent NodeContainer) (NodeContainer, error) {

	n := newShortNode(pb.NodeType_leaf, path, value)
	c := &nodeContainerImp{
		nodeType: pb.NodeType_leaf,
		leaf:     n,
		children: make(map[string]NodeContainer),
		parent:   parent,
	}
	return c, nil
}

func newExtNodeContainer(path string, value []byte, parent NodeContainer) (NodeContainer, error) {
	n := newShortNode(pb.NodeType_extension, path, value)

	c := &nodeContainerImp{
		nodeType: pb.NodeType_extension,
		ext:      n,
		children: make(map[string]NodeContainer),
		parent:   parent,
	}
	return c, nil
}

func newBranchNodeContainer(entries map[byte][]byte, value []byte, parent NodeContainer) (NodeContainer, error) {

	n := newBranchNode(entries, value)

	c := &nodeContainerImp{
		nodeType: pb.NodeType_branch,
		branch:   n,
		children: make(map[string]NodeContainer),
		parent:   parent,
	}

	return c, nil
}

func newNodeFromData(data []byte, parent NodeContainer) (NodeContainer, error) {

	n := &pb.Node{}
	err := proto.Unmarshal(data, n)
	if err != nil {
		return nil, err
	}

	c := &nodeContainerImp{
		children: make(map[string]NodeContainer),
		parent:   parent,
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

////////////////////

func (n *nodeContainerImp) addBranchChild(prefix string, child NodeContainer) error {

	if n.getNodeType() != pb.NodeType_branch {
		return errors.New("node is not a branch node")
	}

	err := n.getBranchNode().addChild(prefix, child.getNodeHash())
	if err != nil {
		return err
	}

	n.children[hex.EncodeToString(child.getNodeHash())] = child

	return nil
}

func (n *nodeContainerImp) removeBranchChild(prefix string) error {
	if n.getNodeType() != pb.NodeType_branch {
		return errors.New("node is not a branch node")
	}

	return n.getBranchNode().removeChild(prefix)
}

func (n *nodeContainerImp) getParent() NodeContainer {
	return n.parent
}

func (n *nodeContainerImp) setParent(p NodeContainer) {
	n.parent = p
}

func (n *nodeContainerImp) getChild(key string) NodeContainer {
	return n.children[key]
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

// Loads node's direct child node(s) to memory from store
func (n *nodeContainerImp) loadChildren(db *leveldb.DB) error {

	if n.nodeType == pb.NodeType_leaf {
		// leaves are childless
		return nil
	}

	if n.nodeType == pb.NodeType_extension {

		// value in an extension node is a pointer to child - load it
		key := n.ext.getValue()

		if n.children[hex.EncodeToString(key)] != nil {
			// already loaded this child
			return nil
		}

		data, err := db.Get(key, nil)
		if err != nil {
			return err
		}

		child, err := newNodeFromData(data, n)
		if err != nil {
			return err
		}

		n.children[hex.EncodeToString(key)] = child
		return nil
	}

	if n.nodeType == pb.NodeType_branch {

		keys := n.branch.getAllChildNodePointers()
		for _, key := range keys {

			if n.children[hex.EncodeToString(key)] != nil {
				// already loaded this child
				continue
			}

			data, err := db.Get(key, nil)
			if err != nil {
				return err
			}
			node, err := newNodeFromData(data, n)
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
