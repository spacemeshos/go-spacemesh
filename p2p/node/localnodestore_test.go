package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNodeLocalStore(t *testing.T) {
	// start clean
	filesystem.SetupTestSpacemeshDataFolders(t, "localnode_store")

	p, err := filesystem.EnsureNodesDataDirectory(config.NodesDirectoryName)
	assert.NoError(t, err, "failed to create or verify nodes data dir")

	err = filesystem.TestEmptyFolder(p)
	assert.NoError(t, err, "There should be no files in the node folder now")

	port1, err := GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	address := fmt.Sprintf("0.0.0.0:%d", port1)

	cfg := config.DefaultConfig()

	node, err := NewNodeIdentity(cfg, address, false)
	assert.NoError(t, err, "failed to create new local node")

	err = node.persistData()
	assert.NoError(t, err, "failed to persist node data")

	_, err = filesystem.EnsureNodeDataDirectory(p, node.ID.String())

	assert.NoError(t, err, "could'nt get node path")

	file := filesystem.NodeDataFile(p, config.NodeDataFileName, node.NodeInfo.ID.String())
	fmt.Println(file)
	exists := filesystem.PathExists(file)

	assert.True(t, exists, "File should exist")

	data, err := readNodeData(node.NodeInfo.ID.String())
	assert.NoError(t, err, "failed to ensure node data directory")
	assert.NotNil(t, data, "expected node data")
	assert.Equal(t, data.PubKey, node.ID.String(), "expected same node id")
	assert.Equal(t, data.NetworkID, cfg.NetworkID, "Expected same network id")

	// as we deleted all dirs - first node data in nodes folder should be this  node's data
	data1, err := readFirstNodeData()
	assert.NoError(t, err, "failed to ensure node data directory")
	assert.NotNil(t, data1, "expected node data")
	assert.Equal(t, data1.PubKey, node.NodeInfo.ID.String(), "expected same node id")
	assert.Equal(t, data1.NetworkID, cfg.NetworkID, "Expected same network id")

	// create a new local node from persisted node data
	node1, err := NewLocalNode(cfg, address, true)
	assert.NoError(t, err, "local node creation error")
	assert.Equal(t, node.NodeInfo.ID.String(), node1.NodeInfo.ID.String(), "expected restored node")
	assert.Equal(t, node.NetworkID(), cfg.NetworkID, "Expected same network id")

	// cleanup
	filesystem.DeleteSpacemeshDataFolders(t)

}
