package merkle

import (
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
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

	keyStr := hex.EncodeToString(k)

	newRoot, err := mt.insert(nil, "", keyStr, v)
	if err != nil {
		return err
	}

	mt.root = newRoot

	return nil
}

// Inserts (k,v) to the tree
// root: current tree (or subtree) root or nil if tree is empty
// k: hex-encoded key to value from root
// prefix: path to root
// returns new root if inserted or error otherwise
func (mt *merkleTreeImp) insert(root NodeContainer, prefix string, k string, v []byte) (NodeContainer, error) {

	if root == nil { // empty tree case

		// new root is a leaf node
		node, err := newLeftNodeContainer(k, v)
		if err != nil {
			return nil, err
		}

		err = mt.persistData(k, v, node)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	// non-empty tree

	// load root direct children if they are not already loaded
	err := root.loadChildren(mt.treeData)
	if err != nil {
		return nil, err
	}

	switch root.getNodeType() {

	case pb.NodeType_leaf:

		// todo: implement me

	case pb.NodeType_branch:

		// get child node for first prefix hex char - child may be null
		k0 := string(k[0])
		childNode := root.getChildren()[k0]

		// insert value to tree rooted w childNode or to an empty tree
		node, err := mt.insert(childNode, prefix+k0, k[1:], v)
		if err != nil {
			return nil, err
		}

		err = root.addBranchChild(k0, node)
		if err != nil {
			return nil, err
		}

		// branch node changed so persist it
		err = mt.persistData(k, v, root)
		if err != nil {
			return nil, err
		}

		return root, nil

	case pb.NodeType_extension:

		// todo: implement me
	}

	return nil, nil
}

// Persist user and tree data for given (userKey, userValue) and NodeContainer
// string - hex encoded key
func (mt *merkleTreeImp) persistData(userKey string, userValue []byte, node NodeContainer) error {

	nodeKey := node.getNodeHash()
	nodeData, err := node.marshal()
	if err != nil {
		return err
	}
	err = mt.treeData.Put(nodeKey, nodeData, nil)
	if err != nil {
		return err
	}

	// todo: think about what about node's children - should they be persisted as well?

	userKeyData, err := hex.DecodeString(userKey)
	if err != nil {
		return err
	}

	err = mt.userData.Put(userKeyData, userValue, nil)
	if err != nil {
		return err
	}

	return err
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
