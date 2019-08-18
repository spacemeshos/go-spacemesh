package nipst

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPostClient(t *testing.T) {
	assert := require.New(t)

	id := make([]byte, 32)
	_, err := rand.Read(id)
	assert.NoError(err)

	c := NewPostClient(&postCfg)
	c.SetParams([]byte("anton"), "/tmp/aaa", 1024)
	assert.NotNil(c)

	idsToCleanup = append(idsToCleanup, id)
	commitment, err := c.initialize(0)
	defer func() { assert.NoError(c.Reset()) }()
	assert.NoError(err)
	assert.NotNil(commitment)

	res, err := verifyPost(commitment, postCfg.SpacePerUnit, postCfg.NumProvenLabels, postCfg.Difficulty)
	assert.NoError(err)
	assert.True(res)

	challenge := []byte("this is a challenge")
	proof, err := c.execute(challenge, 0)
	assert.NoError(err)
	assert.NotNil(proof)
	assert.Equal([]byte(proof.Challenge), challenge[:])

	log.Info("space %v", postCfg.SpacePerUnit)
	res, err = verifyPost(proof, postCfg.SpacePerUnit, postCfg.NumProvenLabels, postCfg.Difficulty)
	assert.NoError(err)
	assert.True(res)
}
