package merkle

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// An mutable branch node
type branchNode interface {
	getValue() []byte                          // value terminated in the path to this node or nil if none exists. Value is key to use-space data
	setValue(v []byte)                         // set the branch value
	getPointer(idx string) []byte              // return pointer to child node for hex char entry or nil. idx - hex char, eg "0", "f"
	marshal() ([]byte, error)                  // to binary data
	getNodeHash() ([]byte, error)              // data hash (determines the pointer to this node)
	getAllChildNodePointers() [][]byte         // get all pointers to child nodes
	addChild(idx string, pointer []byte) error // add a child to this node
	removeChild(idx string) error              // remove a child from this node

	print(userDb *userDb,
		getUserValue func(userDb *userDb,
			v []byte) string) string // returns debug info

}

// Adds a child to the node
func (b *branchNodeImpl) addChild(idx string, pointer []byte) error {

	if len(idx) != 1 {
		return ErrorInvalidHexChar
	}

	i, ok := fromHexChar(idx[0])
	if !ok {
		return ErrorInvalidHexChar
	}

	b.entries[i] = pointer

	// reset hash
	b.nodeHash = []byte{}

	return nil
}

// Removes a child from the node
func (b *branchNodeImpl) removeChild(idx string) error {

	if len(idx) != 1 {
		return ErrorInvalidHexChar
	}

	i, ok := fromHexChar(idx[0])
	if !ok {
		return ErrorInvalidHexChar
	}

	delete(b.entries, i)

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

func newBranchNodeEx(entries map[string][]byte, value []byte) branchNode {

	e := make(map[byte][]byte)

	for k, v := range entries {
		idx, ok := fromHexChar(k[0])
		if ok {
			e[idx] = v
		}
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

func (b *branchNodeImpl) print(userDb *userDb, getUserValue func(userDb *userDb, v []byte) string) string {
	buffer := bytes.Buffer{}
	hashed, err := b.getNodeHash()
	if err != nil {
		buffer.WriteString(fmt.Sprintf("Error printing node with value %s", b.value))
	} else {
		buffer.WriteString(fmt.Sprintf("Branch <%s>.", hex.EncodeToString(hashed)[:6]))
		if len(b.value) > 0 {
			userValue := getUserValue(userDb, b.value)
			buffer.WriteString(fmt.Sprintf(" Stored value: %s. \n", userValue))
		} else {
			buffer.WriteString(" No stored value.\n")
		}

		if len(b.entries) == 0 {
			buffer.WriteString(" No children.\n")
		}

		for k, v := range b.entries {
			if len(v) > 0 {
				ks, _ := toHexChar(k)
				buffer.WriteString(fmt.Sprintf(" [%s] -> <%s>\n", ks, hex.EncodeToString(v)[:6]))
			}
		}
	}

	return buffer.String()
}

// Returns a slice containing all pointers to child nodes
func (b *branchNodeImpl) getAllChildNodePointers() [][]byte {
	res := make([][]byte, 0)
	for _, val := range b.entries {
		if len(val) > 0 {
			res = append(res, val)
		}
	}
	return res
}

func (b *branchNodeImpl) getNodeHash() ([]byte, error) {

	if b.nodeHash == nil || len(b.nodeHash) == 0 { // lazy eval
		d, err := b.marshal()
		if err != nil {
			return nil, err
		}
		b.nodeHash = crypto.Sha256(d)
	}

	return b.nodeHash, nil
}

func (b *branchNodeImpl) getValue() []byte {
	return b.value
}

func (b *branchNodeImpl) setValue(v []byte) {
	b.value = v

	// invalidate node hash as its content just changed
	b.nodeHash = []byte{}
}

func (b *branchNodeImpl) getPointer(idx string) []byte {

	if len(idx) != 1 {
		log.Error("Invalid hex char: %s", idx)
		return []byte{}
	}

	i, ok := fromHexChar(idx[0])

	if !ok {
		log.Error("Invalid hex char: %s", idx)
		return []byte{}
	}

	return b.entries[i]
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
