package p2p

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
)

// GenerateTestNode generates a local test node without persisting data to local store and with default config value.
func GenerateTestNode(t *testing.T) (LocalNode, Peer) {
	return GenerateTestNodeWithConfig(t, nodeconfig.ConfigValues)
}

// GenerateTestNodeWithConfig creates a local test node without persisting data to local store.
func GenerateTestNodeWithConfig(t *testing.T, config nodeconfig.Config) (LocalNode, Peer) {

	port, err := net.GetUnboundedPort()
	if err != nil {
		t.Error("Failed to get a port to bind", err)
	}

	address := fmt.Sprintf("0.0.0.0:%d", port)

	localNode, err := NewNodeIdentity(address, config, false)
	if err != nil {
		t.Error("failed to create local node1", err)
	}

	remoteNode, err := NewRemoteNode(localNode.String(), address)
	if err != nil {
		t.Error("failed to create remote node1", err)
	}

	return localNode, remoteNode
}

// GenerateRandomNodeData generates a remote random node data for testing.
func GenerateRandomNodeData() (node.RemoteNodeData, error) {

	port, err := net.GetUnboundedPort()
	if err != nil {
		return nil, fmt.Errorf("failed to get a port to bind %v", err)
	}

	address := fmt.Sprintf("0.0.0.0:%d", port)
	_, pub, _ := crypto.GenerateKeyPair()
	return node.NewRemoteNodeData(pub.String(), address), nil
}

// GenerateRandomNodesData generates remote nodes data for testing.
func GenerateRandomNodesData(n int) ([]node.RemoteNodeData, error) {
	var err error
	res := make([]node.RemoteNodeData, n)
	for i := 0; i < n; i++ {
		res[i], err = GenerateRandomNodeData()
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}
