package merkle

import (
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"

)

var EmptyTreeRootHash = crypto.Sha256([]byte(""))

func (mt *merkleTreeImp) GetRootHash() []byte {
	if mt.root == nil {
		return EmptyTreeRootHash
	} else {
		return mt.root.getNodeHash()
	}
}

// Returns nil when the tree is empty
func (mt *merkleTreeImp) GetRootNode() NodeContainer {
	return mt.root
}

// Persists user and tree data for given (userKey, userValue) and a NodeContainer (tree-space node)
// node: tree node to store in the tree db
func (mt *merkleTreeImp) persistNode(node NodeContainer) error {

	nodeKey := node.getNodeHash()
	nodeData, err := node.marshal()
	if err != nil {
		return err
	}

	err = mt.treeData.Put(nodeKey, nodeData, nil)
	if err != nil {
		log.Error("Failed to write tree data to db. %v", err)
	}

	return err
}

func (mt *merkleTreeImp) persistUserValue(v []byte) error {
	if len(v) <= 32 {
		return errors.New("Value too small. Epxected len(v) > 32")
	}

	k := crypto.Sha256(v)
	err := mt.userData.Put(k, v, nil)
	if err != nil {
		log.Error("Failed to write user data to db. %v", err)
	}

	log.Info("Persisted %s %s to user db.", hex.EncodeToString(k), hex.EncodeToString(v))

	return nil
}

// remove v keyed by k from the tree
func (mt *merkleTreeImp) Delete(k []byte) error {
	return errors.New("not implemented yet")
}


