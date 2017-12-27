package merkle

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/syndtr/goleveldb/leveldb"
)

type MerkleTree interface {
	Put(k, v []byte)
	Delete(k, v []byte)
	Has(k []byte) bool
	Get(k []byte) ([]byte, bool)

	GetRootHash() []byte
	GetRootNode() NodeContainer
}

type merkleTreeImp struct {
	userData *leveldb.DB
	treeData *leveldb.DB
	root     NodeContainer
}

func NewMerkleTree(userDataFileName string, trieDataFileName string) (MerkleTree, error) {

	userData, err := leveldb.OpenFile(userDataFileName, nil)
	if err != nil {
		return nil, err
	}
	defer userData.Close()

	treeData, err := leveldb.OpenFile(userDataFileName, nil)
	if err != nil {
		return nil, err
	}
	defer treeData.Close()

	mt := &merkleTreeImp{
		userData: userData,
		treeData: treeData,
	}

	// todo: restore merkle root from store

	return mt, nil
}

func (mt *merkleTreeImp) GetRootHash() []byte {
	if mt.root == nil {
		// special case - empty tree with no data
		return crypto.Sha256([]byte(""))
	} else {
		return mt.root.GetNodeHash()
	}
}

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
