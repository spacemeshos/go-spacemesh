package merkle

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/merkle/pb"
)

// Validates integrity of tree rooted at root
// returns hash of root node or error if tree is invalid
func (mt *merkleTreeImp) ValidateStructure(root Node) ([]byte, error) {

	if root == nil {
		return nil, errors.New("expected non-empty root")
	}

	err := root.loadChildren(mt.treeData)
	if err != nil {
		return nil, err
	}

	err = root.validateHash()
	if err != nil {
		return nil, err
	}

	switch root.getNodeType() {

	case pb.NodeType_branch:

		entries := root.getBranchNode().getAllChildNodePointers()
		children := root.getAllChildren()

		if len(entries) != len(children) {
			return nil, fmt.Errorf("mismatch. entries: %d, children: %d", len(entries), len(children))
		}

		for _, c := range children {
			_, err := mt.ValidateStructure(c)
			if err != nil {
				return nil, err
			}
		}

		return root.getNodeHash(), nil

	case pb.NodeType_extension:
		children := root.getAllChildren()
		if len(children) != 1 {
			return nil, errors.New("expected 1 child for extension node")
		}

		childHash, err := mt.ValidateStructure(children[0])
		if err != nil {
			return nil, err
		}

		if !bytes.Equal(childHash, root.getExtNode().getValue()) {
			return nil, errors.New("hash mismatch")
		}

		return root.getNodeHash(), nil

	case pb.NodeType_leaf:

		return root.getNodeHash(), nil
	}

	return nil, errors.New("unexpected node type")
}
