package merkle

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// shortNode is an immutable leaf or an extension node
type shortNode interface {
	isLeaf() bool             // extension node when false
	getValue() []byte         // value for leaf node. Pointer to child node for an extension node
	getPath() string          // hex encoded path to this node from parent
	marshal() ([]byte, error) // to binary data
	getNodeHash() []byte      // node hash - value of pointer to node
}

func newShortNode(nodeType pb.NodeType, path string, parity bool, value []byte) (shortNode, error) {

	node := &shortNodeImpl{
		nodeType: nodeType,
		path:     path,
		value:    value,
	}

	// calc hash of marshaled node data and store
	data, err := node.marshal()
	if err != nil {
		return nil, err
	}
	node.nodeHash = crypto.Sha256(data)
	return node, nil
}

func newShortNodeFromData(data []byte, n *pb.Node) shortNode {

	node := &shortNodeImpl{
		nodeType: n.NodeType,
		path:     n.Path,
		value:    n.Value,
		nodeHash: crypto.Sha256(data),
	}

	return node
}

type shortNodeImpl struct {
	nodeType pb.NodeType // extension node when false
	path     string
	value    []byte
	nodeHash []byte
}

func (s *shortNodeImpl) getNodeHash() []byte { return s.nodeHash }
func (s *shortNodeImpl) getValue() []byte    { return s.value }
func (s *shortNodeImpl) isLeaf() bool        { return s.nodeType == pb.NodeType_leaf }

func (s *shortNodeImpl) getPath() string {
	// todo: consider parity
	return s.path
}

func (s *shortNodeImpl) marshal() ([]byte, error) {

	res := &pb.Node{
		NodeType: s.nodeType,
		Value:    s.value,
		Path:     s.path,
	}

	return proto.Marshal(res)
}
