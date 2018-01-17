package merkle

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// shortNode is an mutable leaf or an extension node
// For a leaf node, value is a key of a value in user-space data store
// For an ext node, value is a pointer to another node in tree-space data store
// In both cases values are sha3s
type shortNode interface {
	isLeaf() bool                 // extension node when false
	getValue() []byte             // value for leaf node. Pointer to child node for an extension node
	getPath() string              // hex encoded path to this node from parent
	marshal() ([]byte, error)     // to binary data
	getNodeHash() ([]byte, error) // node binary data hash - determines the value of pointer to this node
	setValue(v []byte)            // update the node value
	setPath(p string)             // set the path

	print(userDb *userDb, getUserValue func(userDb *userDb, v []byte) string) string // returns debug info
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

func (s *shortNodeImpl) getNodeHash() ([]byte, error) {
	if s.nodeHash == nil || len(s.nodeHash) == 0 { // lazy eval
		// calc hash based on current marshaled node data and store it
		data, err := s.marshal()
		if err != nil {
			return nil, err
		}
		s.nodeHash = crypto.Sha256(data)
	}

	return s.nodeHash, nil
}

func (s *shortNodeImpl) setValue(v []byte) {
	s.value = v

	// invalidate hash
	s.nodeHash = []byte{}
}

func (s *shortNodeImpl) getValue() []byte {
	return s.value
}

func (s *shortNodeImpl) isLeaf() bool {
	return s.nodeType == pb.NodeType_leaf
}

func (s *shortNodeImpl) getPath() string {
	return s.path
}

func (s *shortNodeImpl) setPath(p string) {
	s.path = p

	// rest hash
	s.nodeHash = []byte{}
}

func (s *shortNodeImpl) marshal() ([]byte, error) {
	res := &pb.Node{
		NodeType: s.nodeType,
		Value:    s.value,
		Path:     s.path,
	}

	return proto.Marshal(res)
}

func (s *shortNodeImpl) print(userDb *userDb, getUserValue func(userDb *userDb, v []byte) string) string {
	buffer := bytes.Buffer{}
	hash, err := s.getNodeHash()
	if s.isLeaf() {
		userValue := getUserValue(userDb, s.value)
		if err == nil {
			buffer.WriteString(fmt.Sprintf("Leaf: <%s> path: `%s`, value: `%s`\n",
				hex.EncodeToString(hash)[:6],
				s.path,
				userValue))
		}
	} else {
		if err == nil {
			buffer.WriteString(fmt.Sprintf("Ext: <%s> path: `%s`, value: <%s>\n",
				hex.EncodeToString(hash)[:6],
				s.path,
				hex.EncodeToString(s.value)[:6]))
		}
		if err != nil {
			buffer.WriteString(fmt.Sprintf("Path: `%s`, error: `%s`\n",
				s.path,
				err.Error()))
		}

	}

	return buffer.String()
}
