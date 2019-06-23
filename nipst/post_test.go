package nipst

import (
	"crypto/rand"
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
	numberOfProvenLabels := uint8(proving.NumOfProvenLabels)
	difficulty := proving.Difficulty(5)

	c := NewPostClient()
	assert.NotNil(c)

	idsToCleanup = append(idsToCleanup, id)
	commitment, err := c.initialize(id, space, numberOfProvenLabels, difficulty, 0)
	assert.NoError(err)
	assert.NotNil(commitment)

	res, err := verifyPost(commitment, space, numberOfProvenLabels, difficulty)
	assert.NoError(err)
	assert.True(res)

	challenge := []byte("this is a challenge")
	proof, err := c.execute(id, challenge, numberOfProvenLabels, difficulty, 0)
	assert.NoError(err)
	assert.NotNil(proof)
	assert.Equal([]byte(proof.Challenge), challenge[:])

	res, err = verifyPost(proof, space, numberOfProvenLabels, difficulty)
	assert.NoError(err)
	assert.True(res)
}
