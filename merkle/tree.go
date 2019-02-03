//Package merkle provides a merkle tree which supports CRUD ops for user (k,v) data. It is backed by a (k,v) data store.
//Note that the tree is actually more accurately named trie which is different form the classic definition of a Markle tree - a complete binary tree with values at leaves where each pointer from parent to child is a hash of the child's value  and a non-leaf value is a hash of the union of is pointers to children.
package merkle

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/syndtr/goleveldb/leveldb"
)

// Tree is a general-purpose merkle tree used to store user (k,v) data.
// It is backed by (k,v) data stores.
// All (k,v) methods are in user data space and not in tree space.
// Tree space pointers and paths are internal only.
type Tree interface {
	Put(k, v []byte) error                // store user key, value
	Delete(k []byte) error                // delete user value indexed by key
	Get(k []byte) ([]byte, *stack, error) // get user value indexed by key
	GetRootHash() []byte                  // get tree root hash
	GetRootNode() Node                    // get root node

	CloseDataStores() error // call when done w the tree

	Print() string

	ValidateStructure(root Node) ([]byte, error)
}

type userDb struct {
	*leveldb.DB
}

type treeDb struct {
	*leveldb.DB
}

// internal implementation
type merkleTreeImp struct {
	userData *userDb
	treeData *treeDb
	root     Node
}

// NewEmptyTree creates a new empty Merkle tree with the provided paths to user and tree data db files.
// The db files will be created on these paths if they don't already exist.
// userDataFileName: full local os path and file name for the user data db for this tree
// treeDataFileName: full local os path and file name for the internal tree db store for this tree
func NewEmptyTree(userDataFileName string, treeDataFileName string) (Tree, error) {
	userData, err := leveldb.OpenFile(userDataFileName, nil)
	if err != nil {
		log.Error("Failed to open user db %v", err)
		return nil, err
	}

	treeData, err := leveldb.OpenFile(treeDataFileName, nil)
	if err != nil {
		log.Error("Failed to open tree db %v", err)
		return nil, err
	}

	mt := &merkleTreeImp{
		userData: &userDb{userData},
		treeData: &treeDb{treeData},
	}

	return mt, nil
}

// NewTreeFromDb creates a new tree from provided dbs file paths.
// rootHash: tree root hash - used to pull the root from the db
// userDataFileName: full local os path and file name for user data db for this tree
// treeDataFileName: full local os path and file name for the internal tree db store for this tree
func NewTreeFromDb(rootHash []byte, userDataFileName string, treeDataFileName string) (Tree, error) {

	userData, err := leveldb.OpenFile(userDataFileName, nil)
	if err != nil {
		return nil, err
	}

	treeData, err := leveldb.OpenFile(treeDataFileName, nil)
	if err != nil {
		return nil, err
	}

	mt := &merkleTreeImp{
		userData: &userDb{userData},
		treeData: &treeDb{treeData},
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
