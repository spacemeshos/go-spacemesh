package p2p

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/p2p/node"
	"github.com/UnrulyOS/go-unruly/p2p/nodeconfig"
	"testing"
)

func minInt32(x, y int32) int32 {
	if x < y {
		return x
	}
	return y
}

func minInt(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func minInt64(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}

// Generate a local test node without persisting data to local store and with default config value
func GenerateTestNode(t *testing.T) (LocalNode, Peer) {
	return GenerateTestNodeWithConfig(t, nodeconfig.ConfigValues)
}

// Generate a local test node without persisting data to local store
func GenerateTestNodeWithConfig(t *testing.T, config nodeconfig.Config) (LocalNode, Peer) {

	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("localhost:%d", port)

	localNode, err := NewNodeIdentity(address, config, false)
	if err != nil {
		t.Error("failed to create local node1", err)
	}

	// this will be node 2 view of node 1
	remoteNode, err := NewRemoteNode(localNode.String(), address)
	if err != nil {
		t.Error("failed to create remote node1", err)
	}

	return localNode, remoteNode
}

// Generate a remote random node data for testing
func GenerateRandomNodeData() node.RemoteNodeData {
	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("localhost:%d", port)
	_, pub, _ := crypto.GenerateKeyPair()
	return node.NewRemoteNodeData(pub.String(), address)
}

// Generate remote nodes data for testing
func GenerateRandomNodesData(n int) []node.RemoteNodeData {
	res := make([]node.RemoteNodeData, n)
	for i := 0; i < n; i++ {
		res[i] = GenerateRandomNodeData()
	}
	return res
}
