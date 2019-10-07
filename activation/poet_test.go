package activation

import (
	"crypto/rand"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/poet/integration"
	"github.com/stretchr/testify/require"
	"testing"
)

// newRPCPoetHarnessClient returns a new instance of RPCPoetClient
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
func newRPCPoetHarnessClient() (*RPCPoetClient, error) {
	cfg, err := integration.DefaultConfig()
	if err != nil {
		return nil, err
	}
	cfg.NodeAddress = "NO_BROADCAST"

	h, err := integration.NewHarness(cfg)
	if err != nil {
		return nil, err
	}

	return NewRPCPoetClient(h.PoetClient, h.TearDown), nil
}

type rpcPoetTestCase struct {
	name string
	test func(c *RPCPoetClient, assert *require.Assertions)
}

var rpcPoetTestCases = []*rpcPoetTestCase{
	{name: "RPC poet client", test: testRPCPoetClient},
}

func TestRPCPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	assert := require.New(t)

	c, err := newRPCPoetHarnessClient()
	assert.NotNil(c)
	defer func() {
		err := c.CleanUp()
		assert.NoError(err)
	}()
	assert.NoError(err)

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
	var ch types.Hash32
	_, err := rand.Read(ch[:])
	assert.NoError(err)

	poetRound, err := c.submit(ch)
	assert.NoError(err)
	assert.NotNil(poetRound)
}
