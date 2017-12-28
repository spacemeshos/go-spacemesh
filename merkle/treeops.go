package merkle

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
)

var EmptyTreeRootHash = crypto.Sha256([]byte(""))

func (mt *merkleTreeImp) GetRootHash() []byte {
	if mt.root == nil {
		// special case - empty tree with no data
		return EmptyTreeRootHash
	} else {
		return mt.root.getNodeHash()
	}
}

// Returns nil when the tree is empty
func (mt *merkleTreeImp) GetRootNode() NodeContainer {

	return mt.root
}

// store (k,v)
func (mt *merkleTreeImp) Put(k, v []byte) error {

	if len(v) == 0 || len(k) == 0 {
		return errors.New("Expected non-empty k,v")
	}

	newRoot, err := mt.insert(nil, "", k, v)
	if err != nil {
		return err
	}

	mt.root = newRoot

	return nil
}

// Inserts (k,v) to the tree
// n: current tree (or subtree) root or nil if tree is empty
// returns new root if inserted or error otherwise
func (mt *merkleTreeImp) insert(n NodeContainer, prefix string, k []byte, v []byte) (NodeContainer, error) {

	if n == nil {

	}

	return nil, nil
}

// remove v keyed by k from the tree
func (mt *merkleTreeImp) Delete(k []byte) error {
	return mt.Put(k, nil)
}

// returns true if tree contains key k
func (mt *merkleTreeImp) Has(k []byte) (bool, error) {
	return false, nil
}

// get value associated with key
// returns false if value not found for key k
func (mt *merkleTreeImp) Get(k []byte) ([]byte, bool) {
	return nil, false
}

