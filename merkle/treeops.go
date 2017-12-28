package merkle

import "github.com/spacemeshos/go-spacemesh/crypto"

func (mt *merkleTreeImp) GetRootHash() []byte {
	if mt.root == nil {
		// special case - empty tree with no data
		return crypto.Sha256([]byte(""))
	} else {
		return mt.root.getNodeHash()
	}
}


// Returns nil when the tree is empty
func (mt *merkleTreeImp) GetRootNode() NodeContainer {
	return mt.root
}

// store (k,v)
func (mt *merkleTreeImp) Put(k, v []byte) {

}

// remove (k,v)
func (mt *merkleTreeImp) Delete(k, v []byte) {

}

// returns true if tree contains key k
func (mt *merkleTreeImp) Has(k []byte) bool {
	return false
}

// get value associated with key
// returns false if value not found for key k
func (mt *merkleTreeImp) Get(k []byte) ([]byte, bool) {
	return nil, false
}
