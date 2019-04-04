package nipst

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/poet-ref/integration"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// newRPCPoetHarnessClient returns a new instance of RPCPoetClient
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
func newRPCPoetHarnessClient() (*RPCPoetClient, error) {
	h, err := integration.NewHarness()
	if err != nil {
		return nil, err
	}

	return newRPCPoetClient(h.PoetClient, h.TearDown), nil
}

type rpcPoetTestCase struct {
	name string
	test func(c *RPCPoetClient, assert *require.Assertions)
}

var rpcPoetTestCases = []*rpcPoetTestCase{
	{name: "RPC poet client", test: testRPCPoetClient},
	{name: "RPC poet client timeouts", test: testRPCPoetClientTimeouts},
}

func TestRPCPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	assert := require.New(t)

	c, err := newRPCPoetHarnessClient()
	defer func() {
		err := c.CleanUp()
		assert.NoError(err)
	}()
	assert.NoError(err)
	assert.NotNil(c)

	for _, testCase := range rpcPoetTestCases {
		success := t.Run(testCase.name, func(t1 *testing.T) {
			testCase.test(c, assert)
		})

		if !success {
			break
		}
	}
}

func testRPCPoetClient(c *RPCPoetClient, assert *require.Assertions) {
	var ch common.Hash
	_, err := rand.Read(ch[:])
	assert.NoError(err)

	poetRound, err := c.submit(ch, 10)
	assert.NoError(err)
	assert.NotNil(poetRound)

	mProof, err := c.subscribeMembershipProof(poetRound, ch, 10*time.Second)
	assert.NoError(err)
	assert.NotNil(mProof)
	assert.True(verifyMembership(&ch, mProof))

	proof, err := c.subscribeProof(poetRound, 10*time.Second)
	assert.NoError(err)
	assert.NotNil(proof)
	assert.True(verifyPoet(proof))
	assert.True(verifyPoetMembership(mProof, proof))
}

func testRPCPoetClientTimeouts(c *RPCPoetClient, assert *require.Assertions) {
	var ch common.Hash
	_, err := rand.Read(ch[:])
	assert.NoError(err)

	poetRound, err := c.submit(ch, 10)
	assert.NoError(err)
	assert.NotNil(poetRound)

	mProof, err := c.subscribeMembershipProof(poetRound, ch, 1*time.Millisecond)
	assert.EqualError(err, "deadline exceeded")
	assert.Nil(mProof)

	proof, err := c.subscribeProof(poetRound, 1*time.Millisecond)
	assert.EqualError(err, "deadline exceeded")
	assert.Nil(proof)
}
