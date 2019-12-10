package node

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
	"net"
	"strconv"
	"testing"
	"time"
)

var localhost = net.IP{127, 0, 0, 1}

// ErrFailedToCreate is returned when we fail to create a node
var ErrFailedToCreate = errors.New("failed to create local test node")

// GenerateTestNode generates a local test node without persisting data to local store and with default config value.
func GenerateTestNode(t *testing.T) (*LocalNode, *NodeInfo) {
	return GenerateTestNodeWithConfig(t, config.DefaultConfig())
}

// GenerateTestNodeWithConfig creates a local test node without persisting data to local store.
func GenerateTestNodeWithConfig(t *testing.T, config config.Config) (*LocalNode, *NodeInfo) {

	port, err := GetUnboundedPort()
	if err != nil {
		t.Error("failed to get a port to bind", err)
	}

	address := types.IPAddr{"0.0.0.0", strconv.Itoa(port), "tcp"}

	var localNode *LocalNode

	if config.NodeID != "" {
		localNode, err = NewLocalNode(config, address, false)
		if err != nil {
			t.Error(ErrFailedToCreate)
		}
		return localNode, localNode.NodeInfo
	}

	localNode, err = NewNodeIdentity(config, address, false)
	if err != nil {
		t.Error(ErrFailedToCreate, err)
	}

	return localNode, localNode.NodeInfo
}

// GenerateRandomNodeData generates a remote random node data for testing.
func GenerateRandomNodeData() *NodeInfo {
	rand.Seed(time.Now().UnixNano())
	port := uint16(rand.Int31n(48127) + 1024)
	pub := p2pcrypto.NewRandomPubkey()
	return NewNode(pub, localhost, port, port)
}

// GenerateRandomNodesData generates remote nodes data for testing.
func GenerateRandomNodesData(n int) []*NodeInfo {
	res := make([]*NodeInfo, n)
	for i := 0; i < n; i++ {
		res[i] = GenerateRandomNodeData()
	}
	return res
}

// GetUnboundedPort returns a port that is for sure unbounded or an error.
func GetUnboundedPort() (int, error) {
	l, e := net.Listen("tcp", ":0")
	if e != nil {
		return 0, e
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
