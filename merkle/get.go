package merkle

import (
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
	"github.com/syndtr/goleveldb/leveldb"
)

// Gets user value associated with user key
// returns value if found and nil otherwise
// Returned stack - The tree path path closest to the value
// Returned int - count of matched hex chars on the path
func (mt *merkleTreeImp) Get(k []byte) ([]byte, *stack, error) {

	keyHexStr := hex.EncodeToString(k)

	log.Debug("Merkle get %s ...", keyHexStr)

	s := newStack()

	// get the tree stored user data key to the value
	userValue, err := mt.findValue(mt.root, keyHexStr, 0, s)
	if err != nil {
		log.Error("Error getting user data from m", err)
		return nil, s, err
	}

	if userValue == nil {
		log.Debug("No data in Merkle tree for %s", keyHexStr)
		return nil, s, nil
	}

	log.Debug("Found %s value in Merkle tree for key: %s", hex.EncodeToString(userValue), keyHexStr)

	// pull the data from the user data store
	value, err := mt.userData.Get(userValue, nil)

	if err == leveldb.ErrNotFound {
		// the value from the merkle tree is the short user value - return it
		return userValue, s, nil
	}

	if err != nil {
		return nil, s, err
	}

	// return actual user value
	return value, s, err
}

// Get user value v keyed by k v from the tree
// root: tree root to start looking from
// pos: number of key hex chars (nibbles) already matched and the index in key to start matching from
// k: hex-encoded path (always abs full path)
// s: on return stack of nodes from root to where value should be or is in the tree
func (mt *merkleTreeImp) findValue(root Node, k string, pos int, s *stack) ([]byte, error) {

	if root == nil {
		return nil, nil
	}

	err := root.loadChildren(mt.treeData)
	if err != nil {
		return nil, err
	}

	s.push(root)

	switch root.getNodeType() {
	case pb.NodeType_branch:

		if pos == len(k) { // matched all chars on k
			// return branch node stored value terminated at this path
			return root.getBranchNode().getValue(), nil
		}

		p := root.getBranchNode().getPointer(string(k[pos]))
		if p != nil {
			n := root.getChild(p)

			if n == nil {
				return nil, errors.New("expected to find child")
			}

			return mt.findValue(n, k, pos+1, s)
		}

		return nil, nil

	case pb.NodeType_extension:

		// extension node partial path
		path := root.getExtNode().getPath()
		if len(k)-pos < len(path) || path != k[pos:pos+len(path)] {
			return nil, nil
		}

		p := root.getExtNode().getValue()
		child := root.getChild(p)
		if child == nil {
			return nil, errors.New("expected to find ext node child")
		}

		return mt.findValue(child, k, pos+len(path), s)

	case pb.NodeType_leaf:

		p := root.getLeafNode().getPath()
		if len(k)-pos < len(p) || p != k[pos:pos+len(p)] {
			return nil, nil
		}

		// found leaf for key
		return root.getLeafNode().getValue(), nil
	}

	return nil, nil
}
