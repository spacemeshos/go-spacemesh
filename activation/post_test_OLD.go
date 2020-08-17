package activation

//
//import (
//	"crypto/rand"
//	"github.com/spacemeshos/go-spacemesh/log"
//	"github.com/spacemeshos/go-spacemesh/signing"
//	"github.com/spacemeshos/post/shared"
//	"github.com/stretchr/testify/require"
//	"io/ioutil"
//	"testing"
//)
//
//func TestPostClient(t *testing.T) {
//	assert := require.New(t)
//
//	id := make([]byte, 32)
//	_, err := rand.Read(id)
//	assert.NoError(err)
//
//	postCfg.DataDir, _ = ioutil.TempDir("", "post-test")
//	c, err := NewPostClient(&postCfg, id)
//	assert.NoError(err)
//	assert.NotNil(c)
//
//	err = c.Initialize(CPUProviderId(c.ComputeProviders()))
//	assert.NoError(err)
//	defer func() {
//		err := c.Reset()
//		assert.NoError(err)
//	}()
//	commitment, err := c.Execute(shared.ZeroChallenge)
//	assert.NoError(err)
//
//	keyID := signing.NewPublicKey(id)
//	err = verifyPoST(commitment, keyID.Bytes(), postCfg.NumLabels, postCfg.LabelSize, postCfg.K1, postCfg.K2)
//	assert.NoError(err)
//
//	challenge := []byte("this is a challenge")
//	proof, err := c.Execute(challenge)
//	assert.NoError(err)
//	assert.NotNil(proof)
//	assert.Equal([]byte(proof.Challenge), challenge[:])
//
//	log.Info("numLabels %v", postCfg.NumLabels)
//	err = verifyPoST(proof, keyID.Bytes(), postCfg.NumLabels, postCfg.LabelSize, postCfg.K1, postCfg.K2)
//	assert.NoError(err)
//}
