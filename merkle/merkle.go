package merkle

import (
	"github.com/syndtr/goleveldb/leveldb"
)

type MerkleTree interface {
	Put(k, v []byte)
	Delete(k, v []byte)
	Has(k []byte) bool
	Get(k []byte) ([]byte, bool)
}

type merkleTreeImp struct {
	userData *leveldb.DB
	treeData *leveldb.DB
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

func (mt *merkleTreeImp) Put(k, v []byte) {

}

func (mt *merkleTreeImp) Delete(k, v []byte) {

}

func (mt *merkleTreeImp) Has(k []byte) bool {
	return false
}

func (mt *merkleTreeImp) Get(k []byte) ([]byte, bool) {
	return nil, false
}
