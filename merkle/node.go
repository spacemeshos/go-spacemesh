package merkle

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/syndtr/goleveldb/leveldb"
)

// ErrorInvalidHexChar is returned when a string is not a valid one hex encoded char.
var ErrorInvalidHexChar = errors.New("invalid hex char")

type parent interface {
	// child care
	didLoadChildren() bool
	loadChildren(db *treeDb) error // load all direct children from store
	getChild(pointer []byte) Node
	getAllChildren() []Node
	getChildrenCount() int
	addBranchChild(idx string, child Node) error // idx - hex char
	removeBranchChild(idx string) error
	setExtChild(pointer []byte) error
}

// Node defines the interface for a Merkle tree node.
type Node interface {
	parent

	getNodeType() pb.NodeType
	getLeafNode() shortNode
	getExtNode() shortNode
	getBranchNode() branchNode
	getShortNode() shortNode
	isShortNode() bool
	isLeaf() bool
	isExt() bool
	isBranch() bool
	marshal() ([]byte, error) // get binary encoded marshaled node data
	getNodeHash() []byte
	getNodeEmbeddedPath() string // hex-encoded nibbles or empty

	print(treeDb *treeDb, userDb *userDb) string
	validateHash() error
}

type nodeImp struct {
	nodeType pb.NodeType // contained node type
	leaf     shortNode   // lead node data or nil
	branch   branchNode  // branch node data or nil
	ext      shortNode   // ext node data or nil

	// the only state maintained by nodeContainer is a the runtime parent and node children
	// this info is not db persisted but is computed at runtime and held in nodes loaded to memory
	childrenLoaded bool
	children       map[string]Node // k -pointer to child node (hex encoded). v- child
}

func newLeafNodeContainer(path string, value []byte) (Node, error) {

	n := newShortNode(pb.NodeType_leaf, path, value)
	c := &nodeImp{
		nodeType: pb.NodeType_leaf,
		leaf:     n,
		children: nil,
	}
	return c, nil
}

func newExtNodeContainer(path string, value []byte) (Node, error) {
	n := newShortNode(pb.NodeType_extension, path, value)

	c := &nodeImp{
		nodeType: pb.NodeType_extension,
		ext:      n,
		children: make(map[string]Node),
	}
	return c, nil
}

func newBranchNodeContainer(entries map[byte][]byte, value []byte) (Node, error) {

	n := newBranchNode(entries, value)

	c := &nodeImp{
		nodeType: pb.NodeType_branch,
		branch:   n,
		children: make(map[string]Node),
	}

	return c, nil
}

func newNodeFromData(data []byte) (Node, error) {

	n := &pb.Node{}
	err := proto.Unmarshal(data, n)
	if err != nil {
		return nil, err
	}

	c := &nodeImp{
		children: make(map[string]Node),
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

// setExtChild set's extension node child node
// Pointer: pointer to child (and hash of child's value)
func (n *nodeImp) setExtChild(pointer []byte) error {

	if n.getNodeType() != pb.NodeType_extension {
		return errors.New("node is not a branch node")
	}

	old := n.getExtNode().getValue()
	if old != nil && len(old) > 0 {
		// remove old node from storage here
	}

	n.children = make(map[string]Node)
	n.getExtNode().setValue(pointer)
	n.childrenLoaded = false
	return nil
}

// addBranchChild adds a child node to a branch node
// idx: branch index (hex char representing a nibble) for the new child
// child: child node
// Side effect: if an existing child for index idx exists then it is removed from the tree
func (n *nodeImp) addBranchChild(idx string, child Node) error {

	if n.getNodeType() != pb.NodeType_branch {
		return errors.New("node is not a branch node")
	}

	if len(idx) != 1 {
		return ErrorInvalidHexChar
	}

	// remove node being replaced by this new node
	p := n.getBranchNode().getPointer(idx)
	if len(p) > 0 {
		n.removeBranchChild(idx)
	}

	err := n.getBranchNode().addChild(idx, child.getNodeHash())
	if err != nil {
		return err
	}

	n.children[hex.EncodeToString(child.getNodeHash())] = child

	return nil
}

// removeBranchChild removes a child indexed with idx from a branch node
// todo: figure out if we should always remove the node from backing store. Otherwise, the removed child may never be removed from the local db.
func (n *nodeImp) removeBranchChild(idx string) error {

	if n.getNodeType() != pb.NodeType_branch {
		return errors.New("node is not a branch node")
	}

	if len(idx) != 1 {
		return ErrorInvalidHexChar
	}

	p := n.getBranchNode().getPointer(idx)

	if len(p) > 0 {
		delete(n.children, hex.EncodeToString(p))
	}

	return nil
}

func (n *nodeImp) getChild(pointer []byte) Node {
	if n.children == nil {
		log.Warning("Child not found for pointer: %s", hex.EncodeToString(pointer))
		return nil
	}

	key := hex.EncodeToString(pointer)
	return n.children[key]
}

func (n *nodeImp) getAllChildrenCount() int {
	return len(n.children)
}

func (n *nodeImp) getAllChildren() []Node {
	children := []Node{}
	for _, c := range n.children {
		children = append(children, c)
	}
	return children
}

func (n *nodeImp) getNodeType() pb.NodeType {
	return n.nodeType
}

func (n *nodeImp) getLeafNode() shortNode {
	return n.leaf
}

func (n *nodeImp) getExtNode() shortNode {
	return n.ext
}

// getShortNode returns a shortnode type of this node.
// Returns nil for a branch or unknown node
func (n *nodeImp) getShortNode() shortNode {
	switch n.nodeType {
	case pb.NodeType_branch:
		return nil
	case pb.NodeType_leaf:
		return n.leaf
	case pb.NodeType_extension:
		return n.ext
	default:
		return nil
	}
}

// isLeaf returns true iff node is a leaf node
func (n *nodeImp) isLeaf() bool {
	return n.nodeType == pb.NodeType_leaf
}

// isExt returns true iff node is an extension node
func (n *nodeImp) isExt() bool {
	return n.nodeType == pb.NodeType_extension
}

// isBranch returns true iff node is a branch node
func (n *nodeImp) isBranch() bool {
	return n.nodeType == pb.NodeType_branch
}

// getChildrenCount returns number of current node children.
// Leaf nodes always have 0 children, ext nodes have 1 and branch node can have up to 16 children.
func (n *nodeImp) getChildrenCount() int {
	return len(n.children)
}

// isShortNode returns true iff node is a leaf or an extension node
func (n *nodeImp) isShortNode() bool {
	switch n.nodeType {
	case pb.NodeType_leaf:
		return true
	case pb.NodeType_extension:
		return true
	default:
		return false
	}
}

// validateHash validates node hash without any side effects
func (n *nodeImp) validateHash() error {

	data, err := n.marshal()
	if err != nil {
		return errors.New("failed to marshal data")
	}

	h := crypto.Sha256(data)

	if !bytes.Equal(h, n.getNodeHash()) {
		return errors.New("hash mismatch")
	}

	return nil
}

func (n *nodeImp) getBranchNode() branchNode {
	return n.branch
}

func (n *nodeImp) didLoadChildren() bool {
	return n.childrenLoaded
}

// loadChildren loads node's direct child node(s) to memory from store
func (n *nodeImp) loadChildren(db *treeDb) error {

	if n.nodeType == pb.NodeType_leaf {
		// leaves are childless
		return nil
	}

	if n.childrenLoaded { // already loaded
		return nil
	}

	n.childrenLoaded = true

	if n.nodeType == pb.NodeType_extension {

		// value in an extension node is a pointer to child - load it
		p := n.ext.getValue()

		if n.children[hex.EncodeToString(p)] != nil {
			// already loaded this child
			return nil
		}

		data, err := db.Get(p, nil)
		if err != nil {
			return err
		}

		child, err := newNodeFromData(data)
		if err != nil {
			return err
		}

		n.children[hex.EncodeToString(p)] = child
		return nil
	}

	if n.nodeType == pb.NodeType_branch {

		pointers := n.branch.getAllChildNodePointers()
		for _, p := range pointers {

			if n.children[hex.EncodeToString(p)] != nil {
				// already loaded this child
				continue
			}

			data, err := db.Get(p, nil)
			if err != nil {
				return err
			}

			node, err := newNodeFromData(data)
			if err != nil {
				return err
			}
			n.children[hex.EncodeToString(p)] = node
		}
	}

	return nil
}

func (n *nodeImp) getNodeEmbeddedPath() string {
	switch n.nodeType {
	case pb.NodeType_leaf:
		return n.leaf.getPath()
	case pb.NodeType_extension:
		return n.ext.getPath()
	default:
		return ""
	}
}

func (n *nodeImp) getNodeHash() []byte {
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

func (n *nodeImp) marshal() ([]byte, error) {
	switch n.nodeType {
	case pb.NodeType_branch:
		return n.branch.marshal()
	case pb.NodeType_leaf:
		return n.leaf.marshal()
	case pb.NodeType_extension:
		return n.ext.marshal()
	default:
		return nil, fmt.Errorf("unexpcted node type %d", n.nodeType)
	}
}

func (n *nodeImp) getUserStringValue(userDb *userDb, v []byte) string {

	// pull the data from the user data store
	value, err := userDb.Get(v, nil)
	if err == leveldb.ErrNotFound {
		// the value from the merkle tree is the short user value - return it
		return string(v)
	}

	if err != nil {
		log.Error("Error getting value from db.", err)
		return "<<error>>"
	}

	// long value
	return hex.EncodeToString(value)[:6] + "..."
}

// depth-first-search print tree rooted at node n
// note - this will load the whole tree into memory
func (n *nodeImp) print(treeDb *treeDb, userDb *userDb) string {

	buffer := bytes.Buffer{}

	err := n.loadChildren(treeDb)
	if err != nil {
		buffer.WriteString(fmt.Sprintf("Failed to load children. %v", err))
		return buffer.String()
	}

	switch n.nodeType {
	case pb.NodeType_branch:
		buffer.WriteString(n.getBranchNode().print(userDb, n.getUserStringValue))
		for _, v := range n.children {
			buffer.WriteString(v.print(treeDb, userDb))
		}

	case pb.NodeType_leaf:
		buffer.WriteString(n.getLeafNode().print(userDb, n.getUserStringValue))

	case pb.NodeType_extension:
		buffer.WriteString(n.getExtNode().print(userDb, n.getUserStringValue))
		for _, v := range n.children {
			buffer.WriteString(v.print(treeDb, userDb))
		}
	}

	return buffer.String()
}
