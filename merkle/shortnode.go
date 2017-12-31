package merkle

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// shortNode is an immutable leaf or an extension node
// For a leaf node, value is a key of a value in user-space data store
// For an ext node, value is a pointer to another node in tree-space data store
// In both cases values are sha3s
type shortNode interface {
	isLeaf() bool             // extension node when false
	getValue() []byte         // value for leaf node. Pointer to child node for an extension node
	getPath() string          // hex encoded path to this node from parent
	marshal() ([]byte, error) // to binary data
	getNodeHash() []byte      // node binary data hash - determines the value of pointer to this node
	print() string            // returns debug info

}

func newShortNode(nodeType pb.NodeType, path string, value []byte) shortNode {
	return &shortNodeImpl{
		nodeType: nodeType,
		path:     path,
		value:    value,
	}
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

func (s *shortNodeImpl) getNodeHash() []byte {
	if s.nodeHash == nil || len(s.nodeHash) == 0 { // lazy eval

		// calc hash based on current marshaled node data and store it
		data, err := s.marshal()
		if err != nil {
			return []byte{}
		}
		s.nodeHash = crypto.Sha256(data)
	}

	return s.nodeHash
}

func (s *shortNodeImpl) getValue() []byte { return s.value }
func (s *shortNodeImpl) isLeaf() bool     { return s.nodeType == pb.NodeType_leaf }

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

func (s *shortNodeImpl) print() string {
	buffer := bytes.Buffer{}
	if s.isLeaf() {
		buffer.WriteString("Leaf: ")
	} else {
		buffer.WriteString("Ext: ")
	}

	buffer.WriteString(fmt.Sprintf(" path: %s, value: %s\n", s.path, hex.EncodeToString(s.value)[:6]))
	buffer.WriteString(fmt.Sprintf(" Pointer to node: %s. \n", hex.EncodeToString(s.getNodeHash())[:6]))

	return buffer.String()
}
