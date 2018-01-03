package merkle

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/syndtr/goleveldb/leveldb"
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

func (mt *merkleTreeImp) removeNodeFromStore(node NodeContainer) error {
	nodeKey := node.getNodeHash()
	err := mt.treeData.Delete(nodeKey, nil)
	if err != nil {
		log.Error("failed to delete node from db. %v", err)
		return err
	}
	return nil
}

// Persists user and tree data for given (userKey, userValue) and a NodeContainer (tree-space node)
// node: tree node to store in the tree db
func (mt *merkleTreeImp) persistNode(node NodeContainer) error {

	nodeKey := node.getNodeHash()
	nodeData, err := node.marshal()
	if err != nil {
		log.Error("failed to persist node - invalid data")
		return err
	}

	err = mt.treeData.Put(nodeKey, nodeData, nil)
	if err != nil {
		log.Error("failed to write tree data to db. %v", err)
	}

	log.Info("Persisted node data: %s", node.print(mt.userData, mt.treeData))

	log.Info("Node persisted to tree db. type: %s. db key(node hash): %s", node.getNodeType(), hex.EncodeToString(nodeKey)[:6])

	return nil
}

func (mt *merkleTreeImp) persistUserValue(v []byte) error {
	if len(v) <= 32 {
		return errors.New("value too small. Epxected len(v) > 32")
	}

	k := crypto.Sha256(v)
	err := mt.userData.Put(k, v, nil)
	if err != nil {
		log.Error("failed to write user data to db. %v", err)
		return err
	}

	log.Info("Persisted %s %s to user db.", hex.EncodeToString(k), hex.EncodeToString(v))
	return nil
}

// remove v keyed by k from the tree
func (mt *merkleTreeImp) Delete(k []byte) error {
	return errors.New("not implemented yet")
}

func (mt *merkleTreeImp) CloseDataStores() error {

	err := mt.treeData.Close()
	if err != nil && err != leveldb.ErrClosed {
		log.Error("Failed to close tree db %v", err)
		return err
	}

	err = mt.userData.Close()
	if err != nil && err != leveldb.ErrClosed {
		log.Error("Failed to close user db %v", err)
		return err
	}

	return nil
}

// Print the tree to a string
func (mt *merkleTreeImp) Print() string {
	buffer := bytes.Buffer{}
	buffer.WriteString("\n------------\n")
	if mt.root == nil {
		buffer.WriteString("Merkle Tree: Empty tree.\n")
	} else {

		buffer.WriteString(fmt.Sprintf("Merkle tree: root hash <%s>\n", hex.EncodeToString(mt.GetRootHash())[:6]))
		buffer.WriteString(mt.root.print(mt.userData, mt.treeData))
	}
	buffer.WriteString("------------\n")
	return buffer.String()
}
