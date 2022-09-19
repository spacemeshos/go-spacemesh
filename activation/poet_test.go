package activation

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	r := require.New(t)

	c, err := NewHTTPPoetHarness(true)
	r.NoError(err)
	r.NotNil(c)

	t.Cleanup(func() {
		err := c.Teardown(true)
		if assert.NoError(t, err, "failed to tear down harness") {
			t.Log("harness torn down")
		}
	})

	for _, testCase := range httpPoetTestCases {
		success := t.Run(testCase.name, func(t1 *testing.T) {
			testCase.test(c, r)
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

	poetRound, err := c.Submit(context.TODO(), ch)
	assert.NoError(err)
	assert.NotNil(poetRound)
}
