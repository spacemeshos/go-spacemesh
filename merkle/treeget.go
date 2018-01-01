package merkle

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/syndtr/goleveldb/leveldb"
)

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

		idx, ok := fromHexChar(k[pos])
		if !ok {
			return nil, false, nil, errors.New(fmt.Sprintf("Invalid hex char at index %d of %s", pos, k))
		}

		p := root.getBranchNode().getPointer(idx)
		if p != nil {
			n := root.getChild(p)
			return mt.get(n, k, pos+1)
		}

		return nil, false, nil, nil

	case pb.NodeType_extension:

		// extension node partial path
		path := root.getExtNode().getPath()
		if len(k)-pos < len(path) || path != k[pos:pos+len(path)] {
			return nil, false, nil, nil
		}

		p := root.getExtNode().getValue()
		child := root.getChild(p)
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

