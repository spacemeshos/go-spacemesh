package merkle

import (
	"github.com/syndtr/goleveldb/leveldb"
)

// A general-purpose merkle tree backed by (k,v) stores
// All (k,v) methods are in user data space and not in tree space.
// Tree space pointers and paths are internal only.
type MerkleTree interface {
	Put(k, v []byte) error
	Delete(k []byte) error
	Get(k []byte) ([]byte, bool, error)
	GetRootHash() []byte
	GetRootNode() NodeContainer
}

type merkleTreeImp struct {
	userData *leveldb.DB
	treeData *leveldb.DB
	root     NodeContainer
}

// Creates a new empty merkle tree with the provided paths to user and tree data db files.
// The db files will be created on these pathes if they don't already exist.
// userDataFileName: full local os path and file name for the user data db for this tree
// treeDataFileName: full local os path and file name for the internal tree db store for this tree
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

// Creates a new tree from provided dbs file paths.
// rootHash: tree root hash - used to pull the root from the db
// userDataFileName: full local os path and file name for user data db for this tree
// treeDataFileName: full local os path and file name for the internal tree db store for this tree
func NewTreeFromDb(rootHash []byte, userDataFileName string, treeDataFileName string) (MerkleTree, error) {

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

	root, err := newNodeFromData(data)
	if err != nil {
		return nil, err
	}

	mt.root = root
	return mt, nil
}
