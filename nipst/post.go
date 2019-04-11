package nipst

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/validation"

	"time"
)

type postProof proving.Proof

func (p *postProof) serialize() []byte {
	// TODO(moshababo): implement
	return []byte("")
}

func verifyPost(proof *postProof, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) (bool, error) {
	if space%merkle.NodeSize != 0 {
		return false, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", space, merkle.NodeSize)
	}
	leafCount := space / merkle.NodeSize
	err := validation.Validate(proving.Proof(*proof), leafCount, numberOfProvenLabels, difficulty)
	if err != nil {
		return false, err
	}

	return true, nil
}

type PostClient struct{}

func newPostClient() *PostClient {
	return &PostClient{}
}

func (c *PostClient) initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (commitment *postProof, err error) {
	// TODO(moshababo): implement timeout
	if space%merkle.NodeSize != 0 {
		return nil, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", space, merkle.NodeSize)
	}
	leafCount := space / merkle.NodeSize
	proof, err := initialization.Initialize(id, leafCount, numberOfProvenLabels, difficulty)
	return (*postProof)(&proof), err
}

func (c *PostClient) execute(id []byte, challenge common.Hash, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*postProof, error) {
	// TODO(moshababo): implement timeout
	proof, err := proving.GenerateProof(id, challenge[:], numberOfProvenLabels, difficulty)
	return (*postProof)(&proof), err
}
