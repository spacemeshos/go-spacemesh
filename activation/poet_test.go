package activation

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"testing"
)

type rpcPoetTestCase struct {
	name string
	test func(c *HTTPPoetHarness, assert *require.Assertions)
}

var httpPoetTestCases = []*rpcPoetTestCase{
	{name: "HTTP poet client", test: testHTTPPoetClient},
}

func TestHTTPPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	assert := require.New(t)

	c, err := NewHTTPPoetHarness(true)
	assert.NoError(err)
	assert.NotNil(c)
	defer func() {
		err := c.Teardown(true)
		assert.NoError(err)
	}()

	for _, testCase := range httpPoetTestCases {
		success := t.Run(testCase.name, func(t1 *testing.T) {
			testCase.test(c, assert)
		})

		if !success {
			break
		}
	}
}

func testHTTPPoetClient(c *HTTPPoetHarness, assert *require.Assertions) {
	var ch types.Hash32
	_, err := rand.Read(ch[:])
	assert.NoError(err)

	poetRound, err := c.Submit(ch)
	assert.NoError(err)
	assert.NotNil(poetRound)
}
