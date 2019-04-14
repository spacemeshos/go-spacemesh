package nipst

import (
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/validation"

	"time"
)

type PostProof proving.Proof

func (p *PostProof) String() string {
	return fmt.Sprintf("id: %v, challenge: %v, root: %v",
		bytesToShortString(p.Identity), bytesToShortString(p.Challenge), bytesToShortString(p.MerkleRoot))
}

func bytesToShortString(b []byte) string {
	l := len(b)
	if l == 0 {
		return "empty"
	}
	if l > 5 {
		l = 5
	}
	return fmt.Sprintf("\"%s…\"", hex.EncodeToString(b)[:l])
}

func (p *PostProof) serialize() []byte {
	// TODO(moshababo): implement
	return []byte("")
}

func verifyPost(proof *PostProof, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) (bool, error) {
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

func (c *PostClient) initialize(id []byte, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (commitment *PostProof, err error) {
	// TODO(moshababo): implement timeout
	if space%merkle.NodeSize != 0 {
		return nil, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", space, merkle.NodeSize)
	}
	leafCount := space / merkle.NodeSize
	proof, err := initialization.Initialize(id, leafCount, numberOfProvenLabels, difficulty)
	return (*PostProof)(&proof), err
}

func (c *PostClient) execute(id []byte, challenge common.Hash, numberOfProvenLabels uint8, difficulty proving.Difficulty, timeout time.Duration) (*PostProof, error) {
	// TODO(moshababo): implement timeout
	proof, err := proving.GenerateProof(id, challenge[:], numberOfProvenLabels, difficulty)
	return (*PostProof)(&proof), err
}
