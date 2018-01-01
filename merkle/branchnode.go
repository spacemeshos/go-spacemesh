package merkle

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// An mutable branch (full) node
type branchNode interface {
	getValue() []byte                             // value terminated in the path to this node or nil if none exists. Value is key to use-space data
	setValue(v []byte)                            // set the branch value
	getPointer(prefix byte) []byte                // return pointer to child node for hex char entry or nil
	marshal() ([]byte, error)                     // to binary data
	getNodeHash() []byte                          // data hash (determines the pointer to this node)
	getAllChildNodePointers() [][]byte            // get all pointers to child nodes
	addChild(prefix string, pointer []byte) error // add a child to this node
	removeChild(prefix string) error              // remove a child from this node
	print() string                                // returns debug info
}

// Adds a child to the node
func (b *branchNodeImpl) addChild(prefix string, pointer []byte) error {

	if len(prefix) != 1 {
		return ErrorInvalidHexChar
	}

	idx, ok := fromHexChar(prefix[0])
	if !ok {
		return ErrorInvalidHexChar
	}

	b.entries[idx] = pointer

	// reset hash so it is lazy-computed once it is needed again
	b.nodeHash = []byte{}

	return nil
}

// Removes a child from the node
func (b *branchNodeImpl) removeChild(prefix string) error {

	idx, ok := fromHexChar(prefix[0])
	if !ok {
		return ErrorInvalidHexChar
	}

	delete(b.entries, idx)

	// reset hash
	b.nodeHash = []byte{}

	return nil
}

func newBranchNode(entries map[byte][]byte, value []byte) branchNode {

	e := entries
	if e == nil {
		e = make(map[byte][]byte)
	}

	return &branchNodeImpl{
		value:   value,
		entries: e,
	}
}

// Creates a new branch node from persisted branch node data
func newBranchNodeFromPersistedData(rawData []byte, data *pb.Node) branchNode {

	n := &branchNodeImpl{
		nodeHash: crypto.Sha256(rawData),
		entries:  make(map[byte][]byte),
		value:    data.Value,
	}

	// populate entries table
	for idx, val := range data.Entries {
		if len(val) > 0 {
			n.entries[byte(idx)] = val
		}
	}

	return n
}

type branchNodeImpl struct {
	entries  map[byte][]byte // k- 0-15, value - pointer to child
	value    []byte
	nodeHash []byte
}

func (b *branchNodeImpl) print() string {
	buffer := bytes.Buffer{}

	buffer.WriteString("Branch:\n")

	buffer.WriteString(fmt.Sprintf(" Pointer to node: %s. \n", hex.EncodeToString(b.getNodeHash())[:6]))

	val := hex.EncodeToString(b.value)
	if len(val) > 0 {
		buffer.WriteString(fmt.Sprintf(" Stored value: %s. \n", val[:6]))
	} else {
		buffer.WriteString(" No stored value.\n")

	}
	if len(b.entries) == 0 {
		buffer.WriteString(" No children.\n")
	} else {
		buffer.WriteString(" Children:\n")
	}
	for k, v := range b.entries {
		if len(v) > 0 {
			ks, _ := toHexChar(k)
			buffer.WriteString(fmt.Sprintf(" %s => %s\n", ks, hex.EncodeToString(v)[:6]))
		}
	}

	return buffer.String()
}

func (b *branchNodeImpl) getAllChildNodePointers() [][]byte {
	res := make([][]byte, 0)
	for _, val := range b.entries {
		if len(val) > 0 {
			res = append(res, val)
		}
	}
	return res
}

func (b *branchNodeImpl) getNodeHash() []byte {

	if b.nodeHash == nil || len(b.nodeHash) == 0 { // lazy eval
		d, err := b.marshal()
		if err != nil {
			return []byte{}
		}
		b.nodeHash = crypto.Sha256(d)
	}

	return b.nodeHash
}

func (b *branchNodeImpl) getValue() []byte {
	return b.value
}

func (b *branchNodeImpl) setValue(v []byte) {
	b.value = v

	// invalidate node hash as its content just changed
	b.nodeHash = []byte{}
}

func (b *branchNodeImpl) getPointer(idx byte) []byte {
	return b.entries[idx]
}

func (b *branchNodeImpl) marshal() ([]byte, error) {

	entries := make([][]byte, 16)
	for idx, val := range b.entries {
		entries[idx] = val
	}

	res := &pb.Node{
		NodeType: pb.NodeType_branch,
		Value:    b.value,
		Entries:  entries,
	}

	return proto.Marshal(res)
}
