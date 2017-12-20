package p2p

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/p2p/nodeconfig"
	"testing"
)

func GenerateTestNode(t *testing.T) (LocalNode, Peer) {

	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("localhost:%d", port)

	localNode, err := NewLocalNode(address, nodeconfig.ConfigValues)
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

func GenerateRandomNodeData() RemoteNodeData {
	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("localhost:%d", port)
	_, pub, _ := crypto.GenerateKeyPair()
	return NewRemoteNodeData(pub.String(), address)
}

func GenerateRandomNodesData(n int) []RemoteNodeData {
	res := make([]RemoteNodeData, n)
	for i := 0; i < n; i++ {
		res[i] = GenerateRandomNodeData()
	}
	return res
}
