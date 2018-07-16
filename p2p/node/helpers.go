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
	port := rand.Uint32()

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

// CheckUserPort tries to listen on a port and check whether is usable or not
func CheckUserPort(port uint32) bool {
	address := fmt.Sprintf("0.0.0.0:%d", port)
	l, e := net.Listen("tcp", address)
	if e != nil {
		return true
	}
	l.Close()
	return false
}

// GetUnboundedPort returns a port that is for sure unbounded or an error.
func GetUnboundedPort() (int, error) {
	port := crypto.GetRandomUserPort()
	retryCount := 0
	for used := true; used && retryCount < 10; used, retryCount = CheckUserPort(port), retryCount+1 {
		port = crypto.GetRandomUserPort()
	}
	if retryCount >= 10 {
		return 0, errors.New("failed to establish network, probably no internet connection")
	}
	return int(port), nil
}
