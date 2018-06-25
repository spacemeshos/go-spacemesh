package identity

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNodeLocalStore(t *testing.T) {
	// start clean
	//filesystem.SetupTestSpacemeshDataFolders(t, "localnode_store")

	p, err := filesystem.EnsureNodesDataDirectory(nodeconfig.NodesDirectoryName)
	assert.NoError(t, err, "failed to create or verify nodes data dir")

	err = filesystem.TestEmptyFolder(p)
	assert.NoError(t, err, "There should be no files in the identity folder now")

	port1, err := net.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	address := fmt.Sprintf("0.0.0.0:%d", port1)

	cfg := nodeconfig.DefaultConfig()

	node, err := NewNodeIdentity(cfg, address, false)
	assert.NoError(t, err, "failed to create new local identity")

	err = node.persistData()
	assert.NoError(t, err, "failed to persist identity data")

	_, err = filesystem.EnsureNodeDataDirectory(p, node.String())

	assert.NoError(t, err, "could'nt get node path")

	file := filesystem.NodeDataFile(p, nodeconfig.NodeDataFileName, node.String())
	fmt.Println(file)
	exists := filesystem.PathExists(file)

	assert.True(t, exists, "File should exist")

	data, err := readNodeData(node.String())
	assert.NoError(t, err, "failed to ensure identity data directory")
	assert.NotNil(t, data, "expected identity data")
	assert.Equal(t, data.PubKey, node.String(), "expected same identity id")
	assert.Equal(t, data.NetworkID, cfg.NetworkID, "Expected same network id")

	// as we deleted all dirs - first identity data in nodes folder should be this  identity's data
	data1, err := readFirstNodeData()
	assert.NoError(t, err, "failed to ensure identity data directory")
	assert.NotNil(t, data1, "expected identity data")
	assert.Equal(t, data1.PubKey, node.String(), "expected same identity id")
	assert.Equal(t, data1.NetworkID, cfg.NetworkID, "Expected same network id")

	// create a new local identity from persisted identity data
	node1, err := NewLocalNode(cfg, address, true)
	assert.NoError(t, err, "local identity creation error")
	assert.Equal(t, node.String(), node1.String(), "expected restored identity")
	assert.Equal(t, int(node.NetworkID()), cfg.NetworkID, "Expected same network id")

	// cleanup
	//filesystem.DeleteSpacemeshDataFolders(t)

}
