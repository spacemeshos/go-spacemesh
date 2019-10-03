package nipst

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPostClient(t *testing.T) {
	assert := require.New(t)

	id := make([]byte, 32)
	_, err := rand.Read(id)
	assert.NoError(err)

	c, err := NewPostClient(&postCfg, id)
	assert.NoError(err)
	assert.NotNil(c)

	commitment, err := c.Initialize()
	assert.NoError(err)
	assert.NotNil(commitment)
	defer func() {
		err := c.Reset()
		assert.NoError(err)
	}()

	key_id := signing.NewPublicKey(id)
	err = verifyPost(*key_id, commitment, postCfg.SpacePerUnit, postCfg.NumProvenLabels, postCfg.Difficulty)
	assert.NoError(err)

	challenge := []byte("this is a challenge")
	proof, err := c.Execute(challenge)
	assert.NoError(err)
	assert.NotNil(proof)
	assert.Equal([]byte(proof.Challenge), challenge[:])

	log.Info("space %v", postCfg.SpacePerUnit)
	err = verifyPost(*key_id, proof, postCfg.SpacePerUnit, postCfg.NumProvenLabels, postCfg.Difficulty)
	assert.NoError(err)
}
