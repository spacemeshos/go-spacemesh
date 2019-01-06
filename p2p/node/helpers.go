package node

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"math/rand"
	"net"
	"testing"
	"time"
)

// ErrFailedToCreate is returned when we fail to create a node
var ErrFailedToCreate = errors.New("Failed to create local test node")

// GenerateTestNode generates a local test node without persisting data to local store and with default config value.
func GenerateTestNode(t *testing.T) (*LocalNode, Node) {
	return GenerateTestNodeWithConfig(t, config.DefaultConfig())
}

// GenerateTestNodeWithConfig creates a local test node without persisting data to local store.
func GenerateTestNodeWithConfig(t *testing.T, config config.Config) (*LocalNode, Node) {

	port, err := GetUnboundedPort()
	if err != nil {
		t.Error("Failed to get a port to bind", err)
	}

	address := fmt.Sprintf("0.0.0.0:%d", port)

	var localNode *LocalNode

	if config.NodeID != "" {
		localNode, err = NewLocalNode(config, address, false)
		if err != nil {
			t.Error(ErrFailedToCreate)
		}
		return localNode, Node{localNode.pubKey, address}
	}

	localNode, err = NewNodeIdentity(config, address, false)
	if err != nil {
		t.Error(ErrFailedToCreate, err)
	}

	return localNode, Node{localNode.pubKey, address}
}

// GenerateRandomNodeData generates a remote random node data for testing.
func GenerateRandomNodeData() Node {
	rand.Seed(time.Now().UnixNano())
	port := rand.Int31n(48127) + 1024

	address := fmt.Sprintf("0.0.0.0:%d", port)
	_, pub, _ := crypto.GenerateKeyPair()
	return Node{pub, address}
}

// GenerateRandomNodesData generates remote nodes data for testing.
func GenerateRandomNodesData(n int) []Node {
	res := make([]Node, n)
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
