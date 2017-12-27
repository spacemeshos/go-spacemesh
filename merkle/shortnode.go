package merkle

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"gx/ipfs/QmZ4Qi3GaRbjcx28Sme5eMH7RQjGkt8wHxt2a65oLaeFEV/gogo-protobuf/proto"
)

// shortNode is an immutable leaf or an extension node
type shortNode interface {
	IsLeaf() bool
	GetValue() []byte
	GetPath() string // hex encoded string
	GetParity() bool
	Marshal() ([]byte, error)

	GetNodeHash() []byte
}

func newShortNode(nodeType pb.NodeType, path []byte, parity bool, value []byte) (shortNode, error) {

	node := &shortNodeImpl{
		nodeType: nodeType,
		parity:   parity,
		path:     path,
		value:    value,
	}

	data, err := node.Marshal()
	if err != nil {
		return nil, err
	}

	node.nodeHash = crypto.Sha256(data)
	return node, nil
}

func newShortNodeFromPersistedData(data []byte, n *pb.Node) shortNode {

	node := &shortNodeImpl{
		nodeType: n.NodeType,
		parity:   n.Parity,
		path:     n.Path,
		value:    n.Value,
		nodeHash: crypto.Sha256(data),
	}

	return node
}

type shortNodeImpl struct {
	nodeType pb.NodeType // extension node when false
	parity   bool        // path parity - when odd, truncate first nibble prefix to return path
	path     []byte
	value    []byte

	nodeHash []byte
}

func (s *shortNodeImpl) GetNodeHash() []byte { return s.nodeHash }
func (s *shortNodeImpl) GetValue() []byte    { return s.value }
func (s *shortNodeImpl) GetParity() bool     { return s.parity }
func (s *shortNodeImpl) IsLeaf() bool        { return s.nodeType == pb.NodeType_leaf }

func (s *shortNodeImpl) GetPath() string {
	// todo: consider parity
	return hex.EncodeToString(s.path)
}

func (s *shortNodeImpl) Marshal() ([]byte, error) {

	res := &pb.Node{
		NodeType: s.nodeType,
		Value:    s.value,
		Parity:   s.parity,
		Path:     s.path,
	}

	return proto.Marshal(res)
}
