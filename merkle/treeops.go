package merkle

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/syndtr/goleveldb/leveldb"
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
		return errors.New("expected non-empty k,v for user data")
	}

	// calc the user value to store in the merkle tree
	var userValue []byte
	if len(v) > 32 {
		// if v is long we persist in the user db and store a hash to it (its user-db key) in the merkle tree
		err := mt.persistUserValue(v)
		if err != nil {
			return err
		}
		userValue = crypto.Sha256(v)
	} else {
		// v is short - we just store it in the merkle tree
		userValue = v
	}

	keyStr := hex.EncodeToString(k)

	log.Info("m put user data for key: %s", keyStr )

	newRoot, err := mt.insert(nil, 0, keyStr, userValue)
	if err != nil {
		return err
	}

	mt.root = newRoot
	return nil
}

// Inserts (k,v) to the tree
// root: current tree (or subtree) root or nil if tree is empty
// k: hex-encoded value's key (always abs full path)
// pos: number of nibbles already matched on k to root
// returns root of newly inserted node branch on insert (more than one node may be inserted in 1 iteration)
// returns new root if inserted and nil error, or error otherwise
func (mt *merkleTreeImp) insert(root NodeContainer, pos int, k string, v []byte) (NodeContainer, error) {

	if root == nil {

		node, err := newLeftNodeContainer(k, v, nil)
		if err != nil {
			return nil, err
		}

		err = mt.persistNode(node)
		if err != nil {
			return nil, err
		}

		return node, nil
	}

	// non-empty tree at root  - load root direct children if they are not already loaded
	err := root.loadChildren(mt.treeData)
	if err != nil {
		return nil, err
	}

	switch root.getNodeType() {

	case pb.NodeType_leaf:
		fallthrough
	case pb.NodeType_extension:

		/// example:
		/// K: 0123456789
		/// pos: 2 (01 matched)
		/// leaf-path: 23455789
		/// cp: 2345
		/// lcp: 4

		/// ext: key: 2345 -> branch
		/// branch childs:
		/// l1 6 -> 789 (new path leaf), v. branch insert: pos: 6, k
		/// l2 5 -> 789, old leaf val. v branch insert: pos 6, shared prefix + old-leaf k

		if bytes.Equal(root.getLeafNode().getValue(), v) { // value already in this leaf
			return root, nil
		}

		cp := commonPrefix(root.getNodeEmbeddedPath(), k[pos:])
		lcp := len(cp)

		// create a branch + 1 existing updated node (ext or leaf) + new leaf node

		b, err := newBranchNodeContainer(nil, nil, root)
		if err != nil {
			return nil, err
		}

		if root.getNodeType() == pb.NodeType_extension {

			extPath := root.getNodeEmbeddedPath() // e.g. 23455789

			prefixChar := string(extPath[lcp])      // first hex char for path e.g 5
			p := extPath[lcp+1:]                    // remaining path - e.g. 789
			pointer := root.getExtNode().getValue() // ext node pointer to child

			newExtNode, err := newExtNodeContainer(p, pointer, b)
			if err != nil {
				return nil, err
			}
			b.addBranchChild(prefixChar, newExtNode)

		} else {
			// 	_, branch.Children[n.Key[matchlen]], err = t.insert(nil, append(prefix, n.Key[:matchlen+1]...), n.Key[matchlen+1:], n.Val)
			// existing leaf inserted into branch
			_, err = mt.insert(b, pos+lcp, k[:pos]+root.getNodeEmbeddedPath(), root.getLeafNode().getValue())
			if err != nil {
				return nil, err
			}
		}

		// 	_, branch.Children[key[matchlen]], err = t.insert(nil, append(prefix, key[:matchlen+1]...), key[matchlen+1:], value)
		_, err = mt.insert(b, pos+lcp, k, v)
		if err != nil {
			return nil, err
		}

		if len(cp) == 0 { // no need to return extension node as there's no shared prefix
			return b, nil
		}

		// add ext node child of root with common prefix w branch as child
		ext, err := newExtNodeContainer(k[pos:pos+lcp], b.getNodeHash(), root)

		// persist k,v and ext node
		err = mt.persistNode(ext)
		if err != nil {
			return nil, err
		}

		// remove replaced leaf or ext node
		err = mt.treeData.Delete(root.getNodeHash(), nil)
		if err != nil {
			return nil, err
		}

		// return newly added ext node
		return ext, nil

	case pb.NodeType_branch:

		// save the root key as hash is about to change
		oldKey := root.getNodeHash()

		// k: 01234
		if pos == len(k) {
			// we matched the whole key and got to a branch node - save value in the value field
			root.getBranchNode().setValue(v)
		} else {
			// get child node for first prefix hex char - child may be nil
			idx := string(k[pos])
			childNode := root.getChild(idx)

			// insert value to tree rooted w childNode or to an empty tree
			node, err := mt.insert(childNode, pos+1, k, v)
			if err != nil {
				return nil, err
			}

			err = root.addBranchChild(idx, node)
			if err != nil {
				return nil, err
			}
		}

		// update pointers all the way to the root of the tree
		//parent := root.getParent()
		//if (parent != nil) {
		//	parent.updateChildPointer(oldKey, root)
		//}

		// todo: when a branch node changes, all the pointers from it up to the root change
		// and needs to get updated to keep the trie correct - is this post-processing or recursive?

		// branch node changed so persist it
		err = mt.persistNode(root)
		if err != nil {
			return nil, err
		}

		// remove root from keystore indexed by its older hash - it is now saved with the new hash
		err = mt.treeData.Delete(oldKey, nil)
		if err != nil {
			return nil, err
		}

		return root, nil

	}
	return nil, nil
}

// get value associated with key
// returns error on internal error. flase if not found
func (mt *merkleTreeImp) Get(k []byte) ([]byte, bool, error) {

	keyHexStr := hex.EncodeToString(k)

	log.Info("m get %s ...", keyHexStr)

	// get the tree stored user data key to the value
	userValue, found, _, err := mt.get(mt.root, keyHexStr, 0)
	if err != nil {
		log.Error("Error getting user data from m. %v", err)
		return nil, false, err
	}

	if !found {
		log.Info("No data in m for %s", keyHexStr)
		return nil, false, nil
	}

	log.Info("Found %s value in merkle tree for key: %s", hex.EncodeToString(userValue), keyHexStr)

	// pull the data from the user data store
	value, err := mt.userData.Get(userValue, nil)

	if err == leveldb.ErrNotFound {
		// the value from the merkle tree is the short user valuevalue
		return userValue, true, nil
	}

	if err != nil {
		return nil, false, err
	}

	return value, true, err
}

// Get user value v keyed by k v from the tree
// (origNode node, key []byte, pos int) (value []byte, newnode node, didResolve bool, err error)
// pos: number of key hex chars (nibbles) already matched and the index in key to start matching from
// k: hex encoded key
func (mt *merkleTreeImp) get(root NodeContainer, k string, pos int) ([]byte, bool, NodeContainer, error) {

	if root == nil {
		return nil, false, nil, nil
	}

	root.loadChildren(mt.treeData)

	switch root.getNodeType() {
	case pb.NodeType_branch:

		if pos == len(k)-1 {
			// return branch node stored value terminated at this path
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
	return err
}

// remove v keyed by k from the tree
func (mt *merkleTreeImp) Delete(k []byte) error {
	return errors.New("not implemented yet")
}
