package nipst

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/post/proving"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPostClient(t *testing.T) {
	assert := require.New(t)

	id := make([]byte, 32)
	_, err := rand.Read(id)
	assert.NoError(err)

	space := uint64(1024)
	numberOfProvenLabels := uint8(proving.NumberOfProvenLabels)
	difficulty := proving.Difficulty(5)

	c := newPostClient()
	assert.NotNil(c)

	commitment, err := c.initialize(id, space, numberOfProvenLabels, difficulty, 0)
	assert.NoError(err)
	assert.NotNil(commitment)

	res, err := verifyPost(commitment, space, numberOfProvenLabels, difficulty)
	assert.NoError(err)
	assert.True(res)

	challenge := common.BytesToHash([]byte("this is a challenge"))
	proof, err := c.execute(id, challenge, numberOfProvenLabels, difficulty, 0)
	assert.NoError(err)
	assert.NotNil(proof)

	res, err = verifyPost(proof, space, numberOfProvenLabels, difficulty)
	assert.NoError(err)
	assert.True(res)
}
