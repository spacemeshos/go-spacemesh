package merkle

import (
	"errors"
)

// remove v keyed by k from the tree
func (mt *merkleTreeImp) Delete(k []byte) error {

	return errors.New("not implemented yet")

	//	if len(k) == 0 {
	//		return ErrorInvalidUserData
	//	}
	//
	//	// first, attempt to find the value in the tree and return path to where value should be added
	//	// in the case it is not already in the tree
	//	res, stack, err := mt.Get(k)
	//
	//	if len(res) == 0 || err != nil {
	//		return nil
	//	}
	//
	//	mt.deleteUserValueFromStorge(res)
	//
	//	hexKey := hex.EncodeToString(k)
	//	log.Info("m Deleting user data for key: %s...", hexKey)
	//
	//	err = mt.delete(hexKey, stack)
	//	if err != nil {
	//		return err
	//	}
	//
	//	return nil
}

// Deletes value from node at top of the stack from the tree
// Stack contains a path to a node to be deleted
// eg. ext -> branch -> ext -> branch
// k: path to path last node (top of stack)
// s: path to node to be deleted from tree root
func (mt *merkleTreeImp) delete(k string, s *stack) error {

	lastNode := s.pop()

	if lastNode == nil {
		return nil
	}

	parentNode := s.pop()
	if parentNode == nil {
		// tree with 1 leaf - remove leaf and set to empty tree
		mt.removeNodeFromStore(lastNode)
		mt.root = nil
		return nil
	}

	if lastNode.isBranch() {
		// last node is a branch - clear its value
		mt.removeNodeFromStore(lastNode)
		lastNode.getBranchNode().setValue(nil)
		mt.persistNode(lastNode)
	} else {
		// last node is a leaf and leaf parent must be a branch
		path := lastNode.getNodeEmbeddedPath()

		k = k[:len(k)-len(path)]
		mt.removeNodeFromStore(lastNode)

		// get the removed leaf idx from the key
		idx := string(k[len(k)-1])

		// remove the leaf from the branch
		parentNode.removeBranchChild(idx)

		// update keys, last node and parent node
		k = k[:len(k)-2]
		lastNode = parentNode
		parentNode = s.pop()
	}

	// last node is a branch. if there's only 1 child we need
	// to collapse it to an ext node

	if lastNode.getChildrenCount() == 1 {
		// we need to add the node below current branch node
		// to the node above it
	} else {
		s.push(parentNode)
	}

	s.push(lastNode)

	// update all pointers in the path specified by stack
	mt.update(k, s)

	return nil
}
