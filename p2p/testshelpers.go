package p2p

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
)

// Generates a local test node without persisting data to local store and with default config value
func GenerateTestNode(t *testing.T) (LocalNode, Peer) {
	return GenerateTestNodeWithConfig(t, nodeconfig.ConfigValues)
}

// Generates a local test node without persisting data to local store
func GenerateTestNodeWithConfig(t *testing.T, config nodeconfig.Config) (LocalNode, Peer) {

	port := crypto.GetRandomUInt32(10000) + 1000
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

// Generates a remote random node data for testing
func GenerateRandomNodeData() node.RemoteNodeData {
	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("0.0.0.0:%d", port)
	_, pub, _ := crypto.GenerateKeyPair()
	return node.NewRemoteNodeData(pub.String(), address)
}

// Generates remote nodes data for testing
func GenerateRandomNodesData(n int) []node.RemoteNodeData {
	res := make([]node.RemoteNodeData, n)
	for i := 0; i < n; i++ {
		res[i] = GenerateRandomNodeData()
	}
	return res
}
