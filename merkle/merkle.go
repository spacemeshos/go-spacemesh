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

// Create a new empty merkle tree
func NewEmptyTree(userDataFileName string, treeDataFileName string) (MerkleTree, error) {
	userData, err := leveldb.OpenFile(userDataFileName, nil)
	if err != nil {
		return nil, err
	}
	defer userData.Close()

	treeData, err := leveldb.OpenFile(treeDataFileName, nil)
	if err != nil {
		return nil, err
	}
	defer treeData.Close()

	mt := &merkleTreeImp{
		userData: userData,
		treeData: treeData,
	}

	return mt, nil
}

// Creates a new tree from provided dbs
// rootHash: tree root hash - used to pull the root from the db
// loadChilds: set to true to load all the tree to memory. Set to false for lazy loading of nodes from the db
func NewTreeFromStore(rootHash []byte, userDataFileName string, treeDataFileName string, loadChilds bool) (MerkleTree, error) {

	userData, err := leveldb.OpenFile(userDataFileName, nil)
	if err != nil {
		return nil, err
	}
	defer userData.Close()

	treeData, err := leveldb.OpenFile(treeDataFileName, nil)
	if err != nil {
		return nil, err
	}
	defer treeData.Close()

	mt := &merkleTreeImp{
		userData: userData,
		treeData: treeData,
	}

	// load the tree from the db

	data, err := treeData.Get(rootHash, nil)
	if err != nil {
		return nil, err
	}

	root, err := nodeFromData(data)
	if err != nil {
		return nil, err
	}

	if loadChilds {
		err = root.loadChildren(treeData)
		if err != nil {
			return nil, err
		}
	}

	mt.root = root
	return mt, nil
}

func (mt *merkleTreeImp) GetRootHash() []byte {
	if mt.root == nil {
		// special case - empty tree with no data
		return crypto.Sha256([]byte(""))
	} else {
		return mt.root.getNodeHash()
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
