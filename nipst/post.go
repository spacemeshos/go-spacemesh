package nipst

import (
	"github.com/spacemeshos/go-spacemesh/common"
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

func verifyPost(proof *postProof, leafCount uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) (bool, error) {
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
	proof, err := initialization.Initialize(id, space, numberOfProvenLabels, difficulty)
	return (*postProof)(&proof), err
}

func (c *PostClient) execute(id []byte, challenge common.Hash, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*postProof, error) {
	// TODO(moshababo): implement timeout
	proof, err := proving.GenerateProof(id, challenge[:], numberOfProvenLabels, difficulty)
	return (*postProof)(&proof), err
}
