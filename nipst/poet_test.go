package nipst

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type rpcPoetTestCase struct {
	name string
	test func(c *RPCPoetHarness, assert *require.Assertions)
}

var rpcPoetTestCases = []*rpcPoetTestCase{
	{name: "RPC poet harness", test: testRPCPoetHarness},
	{name: "RPC poet harness timeouts", test: testRPCPoetHarnessTimeouts},
}

func TestRPCPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	assert := require.New(t)

	c, err := newRPCPoetHarness()
	defer func() {
		err := c.cleanUp()
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

func testRPCPoetHarness(c *RPCPoetHarness, assert *require.Assertions) {
	var ch common.Hash
	_, err := rand.Read(ch[:])
	assert.NoError(err)

	poetRound, err := c.submit(ch, 10)
	assert.NoError(err)
	assert.NotNil(poetRound)

	mProof, err := c.subscribeMembershipProof(poetRound, ch, 10*time.Second)
	assert.NoError(err)
	assert.NotNil(mProof)
	assert.True(mProof.valid())

	proof, err := c.subscribeProof(poetRound, 10*time.Second)
	assert.NoError(err)
	assert.NotNil(proof)
	assert.True(proof.valid())
}

func testRPCPoetHarnessTimeouts(c *RPCPoetHarness, assert *require.Assertions) {
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
