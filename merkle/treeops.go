package merkle

import (
	"bytes"
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
		return errors.New("expected non-empty k,v")
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
// k: hex-encoded value's key
// prefix: path to root

// returns new root if inserted or error otherwise
func (mt *merkleTreeImp) insert(root NodeContainer, prefix string, k string, v []byte) (NodeContainer, error) {

	if root == nil {
		node, err := newLeftNodeContainer(k, v, nil)
		if err != nil {
			return nil, err
		}

		err = mt.persistData(k, v, node)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	// non-empty tree  - load root direct children if they are not already loaded
	err := root.loadChildren(mt.treeData)
	if err != nil {
		return nil, err
	}

	switch root.getNodeType() {

	case pb.NodeType_leaf:

		if bytes.Equal(root.getLeafNode().getValue(), v) { // value already in this leaf
			return root, nil
		}

		cp := commonPrefix(root.getLeafNode().getPath(), k)
		lcp := len(cp)


		// create 2 leaf nodes

		b, err := newBranchNodeContainer(nil, nil, root)
		if err != nil {
			return nil, err
		}

		// 	_, branch.Children[n.Key[matchlen]], err = t.insert(nil, append(prefix, n.Key[:matchlen+1]...), n.Key[matchlen+1:], n.Val)
		_, err = mt.insert(b, prefix + root.getLeafNode().getPath()[:lcp+1], root.getLeafNode().getPath()[lcp+1:], root.getLeafNode().getValue())
		if err != nil {
			return nil, err
		}

		// 	_, branch.Children[key[matchlen]], err = t.insert(nil, append(prefix, key[:matchlen+1]...), key[matchlen+1:], value)
		_, err = mt.insert(b, prefix + root.getLeafNode().getPath()[:lcp+1], k[lcp+1:], v)
		if err != nil {
			return nil, err
		}

		if len(cp) == 0 { // no need to return extension node as there's no shared prefix
			return b, nil
		}

		// todo: add extension node with common prefix w branch as child and return it

		ext, err := newExtNodeContainer(k[:lcp], b.getNodeHash(), root)

		// persist k,v and ext node
		err = mt.persistData(k, v, ext)
		if err != nil {
			return nil, err
		}

		// remove replaced leaf node
		err = mt.treeData.Delete(root.getNodeHash(), nil)
		if err != nil {
			return nil, err
		}

		// return newly added ext node
		return ext, nil


	case pb.NodeType_branch:

		// save the key as hash is about to change
		oldKey := root.getNodeHash()

		if len(prefix) == len(k) {
			// we matched the whole key and got to a branch node - save value in the value field
			root.getBranchNode().setValue(v)
		} else {
			// get child node for first prefix hex char - child may be nil
			idx := string(k[len(prefix)])
			childNode := root.getChild(idx)

			// insert value to tree rooted w childNode or to an empty tree
			node, err := mt.insert(childNode, prefix+idx, k, v)
			if err != nil {
				return nil, err
			}

			err = root.addBranchChild(idx, node)
			if err != nil {
				return nil, err
			}
		}

		// todo: when a branch node changes, all the pointers from it up to the root change
		// and needs to get updated to keep the trie correct - is this post-processing or recursive?

		// branch node changed so persist it
		err = mt.persistData(k, v, root)
		if err != nil {
			return nil, err
		}

		// remove root from keystore indexed by its older hash - it is now saved with the new hash
		err = mt.treeData.Delete(oldKey, nil)
		if err != nil {
			return nil, err
		}

		return root, nil

	case pb.NodeType_extension:

		// todo: implement me
	}

	return nil, nil
}

// get value associated with key
// returns error on internal error. flase if not found
func (mt *merkleTreeImp) Get(k []byte) ([]byte, bool, error) {

	// get the tree stored user data key to the value
	key, found, _, err := mt.get(mt.root, hex.EncodeToString(k), 0)
	if err != nil {
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	// pull the data from the user data store
	value, err := mt.userData.Get(key, nil)
	if err != nil {
		return nil, false, err
	}

	return value, true, err
}

// (origNode node, key []byte, pos int) (value []byte, newnode node, didResolve bool, err error)
// pos - number of key hex chars already matched and the index in key to start matching from
func (mt *merkleTreeImp) get(root NodeContainer, k string, pos int) ([]byte, bool, NodeContainer, error) {

	if root == nil {
		return nil, false, nil, nil
	}
	root.loadChildren(mt.treeData)

	switch root.getNodeType() {
	case pb.NodeType_branch:

		if pos == len(k)-1 {
			// return branch node value terminated at this path
			return root.getBranchNode().getValue(), true, root, nil
		}

		childNode := root.getChild(string(k[0]))
		return mt.get(childNode, k, pos+1)

	case pb.NodeType_extension:

		// extension node partial path
		path := root.getExtNode().getPath()
		if len(k)-pos < len(path) || path != k[pos:pos+len(path)] {
			return nil, false, nil, nil
		}

		pointer := hex.EncodeToString(root.getExtNode().getValue())
		child := root.getChild(pointer)
		return mt.get(child, k, pos+len(path))

	case pb.NodeType_leaf:

		p := root.getLeafNode().getPath()
		if len(k)-pos < len(p) || p != k[pos:pos+len(p)] {
			return nil, false, nil, nil
		}

		// found
		return root.getLeafNode().getValue(), true, root, nil
	}

	return nil, false, nil, nil
}

// Persists user and tree data for given (userKey, userValue) and a NodeContainer (tree-space node)
// userKey: hex encoded user-space key
// userValue: value to store in the user db
// node: tree node to store in the tree db
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
	return nil
}
