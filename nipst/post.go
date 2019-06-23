package nipst

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/validation"

	"time"
)

func verifyPost(proof *types.PostProof, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) (bool, error) {
	if space%merkle.NodeSize != 0 {
		return false, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", space, merkle.NodeSize)
	}
	err := validation.Validate(proving.Proof(*proof), proving.Space(space), numberOfProvenLabels, difficulty)
	if err != nil {
		return false, err
	}

	return true, nil
}

type PostClient struct{}

func NewPostClient() *PostClient {
	return &PostClient{}
}

func (c *PostClient) initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (commitment *types.PostProof, err error) {
	// TODO(moshababo): implement timeout
	//TODO: implement persistence
	if space%merkle.NodeSize != 0 {
		return nil, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", space, merkle.NodeSize)
	}
	proof, err := initialization.Initialize(id, proving.Space(space), numberOfProvenLabels, difficulty)
	return (*types.PostProof)(&proof), err
}

func (c *PostClient) execute(id []byte, challenge []byte, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*types.PostProof, error) {
	// TODO(moshababo): implement timeout
	proof, err := proving.GenerateProof(id, challenge, numberOfProvenLabels, difficulty)
	return (*types.PostProof)(&proof), err
}
