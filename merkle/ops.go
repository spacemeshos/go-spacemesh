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

// EmptyTreeRootHash is the hash used to represent an empty Merkle tree.
var EmptyTreeRootHash = crypto.Sha256([]byte(""))

// GetRootHash gets the has of the Merkle tree root.
func (mt *merkleTreeImp) GetRootHash() []byte {
	if mt.root == nil {
		return EmptyTreeRootHash
	}
	return mt.root.getNodeHash()

}

// GetRootNode returns the Merkle tree root node or nil when the tree is empty.
func (mt *merkleTreeImp) GetRootNode() Node {
	return mt.root
}

func (mt *merkleTreeImp) removeNodeFromStore(node Node) error {
	nodeKey := node.getNodeHash()
	err := mt.treeData.Delete(nodeKey, nil)
	if err != nil {
		log.Error("Failed to delete node from db", err)
		return err
	}
	return nil
}

// Persists user and tree data for given (userKey, userValue) and a Node (tree-space node)
// node: tree node to store in the tree db
func (mt *merkleTreeImp) persistNode(node Node) error {

	nodeKey := node.getNodeHash()
	nodeData, err := node.marshal()
	if err != nil {
		log.Error("Failed to persist node - invalid data")
		return err
	}

	err = mt.treeData.Put(nodeKey, nodeData, nil)
	if err != nil {
		log.Error("Failed to write tree data to db", err)
	}

	log.Debug("Persisted node data: %s", node.print(mt.treeData, mt.userData))
	log.Debug("Node persisted to tree db. type: %s. db key(node hash): %s", node.getNodeType(), hex.EncodeToString(nodeKey)[:6])
	return nil
}

func (mt *merkleTreeImp) deleteUserValueFromStore(v []byte) error {
	if len(v) <= 32 {
		return errors.New("value too small. Epxected len(v) > 32")
	}

	k := crypto.Sha256(v)
	err := mt.userData.Delete(k, nil)
	if err != nil {
		log.Error("Failed to delete from user data to db", err)
		return err
	}

	log.Debug("Removed %s %s from user db.", hex.EncodeToString(k), hex.EncodeToString(v))
	return nil
}

func (mt *merkleTreeImp) persistUserValue(v []byte) error {
	if len(v) <= 32 {
		return errors.New("value too small. Epxected len(v) > 32")
	}

	k := crypto.Sha256(v)
	err := mt.userData.Put(k, v, nil)
	if err != nil {
		log.Error("Failed to write user data to db", err)
		return err
	}

	log.Debug("Persisted %s %s to user db.", hex.EncodeToString(k), hex.EncodeToString(v))
	return nil
}

func (mt *merkleTreeImp) CloseDataStores() error {

	err := mt.treeData.Close()
	if err != nil && err != leveldb.ErrClosed {
		log.Error("Failed to close tree db", err)
		return err
	}

	err = mt.userData.Close()
	if err != nil && err != leveldb.ErrClosed {
		log.Error("Failed to close user db", err)
		return err
	}

	return nil
}

// Prints the tree to a string
func (mt *merkleTreeImp) Print() string {
	buffer := bytes.Buffer{}
	buffer.WriteString("\n------------\n")
	if mt.root == nil {
		buffer.WriteString("Merkle Tree: Empty tree.\n")
	} else {

		buffer.WriteString(fmt.Sprintf("Merkle tree: root hash <%s>\n", hex.EncodeToString(mt.GetRootHash())[:6]))
		buffer.WriteString(mt.root.print(mt.treeData, mt.userData))
	}
	buffer.WriteString("------------\n")
	return buffer.String()
}
