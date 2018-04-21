package p2p

import (
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
)

func TestNodeLocalStore(t *testing.T) {
	// start clean
	filesystem.SetupTestSpacemeshDataFolders(t, "localnode_store")

	p, err := ensureNodesDataDirectory()
	assert.NoErr(t, err, "failed to create or verify nodes data dir")

	err = filesystem.TestEmptyFolder(p)
	assert.NoErr(t, err, "There should be no files in the node folder now")

	port1, err := net.GetUnboundedPort()
	assert.NoErr(t, err, "Should be able to establish a connection on a port")

	address := fmt.Sprintf("0.0.0.0:%d", port1)

	node, err := NewNodeIdentity(address, nodeconfig.ConfigValues, false)
	assert.NoErr(t, err, "failed to create new local node")

	err = node.persistData()
	assert.NoErr(t, err, "failed to persist node data")

	_, err = node.EnsureNodeDataDirectory()
	assert.NoErr(t, err, "failed to ensure node data directory")

	// shutdown node as we'd like to start a new one with same ip:port on local host
	node.Shutdown()

	// Wait until node shuts down and stops listening on the the node's port to make CI happy
	time.Sleep(time.Second * 5)

	file, err := getDataFilePath(node.String())

	assert.NoErr(t, err, "could'nt get file path")

	exists := filesystem.PathExists(file)

	assert.True(t, exists, "File should exists after shutdown")

	data, err := readNodeData(node.String())
	assert.NoErr(t, err, "failed to ensure node data directory")
	assert.NotNil(t, data, "expected node data")
	assert.Equal(t, data.PubKey, node.String(), "expected same node id")
	assert.Equal(t, data.NetworkID, node.Config().NetworkID, "Expected same network id")

	// as we deleted all dirs - first node data in nodes folder should be this  node's data
	data1, err := readFirstNodeData()
	assert.NoErr(t, err, "failed to ensure node data directory")
	assert.NotNil(t, data1, "expected node data")
	assert.Equal(t, data1.PubKey, node.String(), "expected same node id")
	assert.Equal(t, data1.NetworkID, node.Config().NetworkID, "Expected same network id")

	// create a new local node from persisted node data
	node1, err := NewLocalNode(address, nodeconfig.ConfigValues, true)
	assert.NoErr(t, err, "local node creation error")
	assert.Equal(t, node.String(), node1.String(), "expected restored node")
	assert.Equal(t, node.Config().NetworkID, node1.Config().NetworkID, "Expected same network id")

	// cleanup
	filesystem.DeleteSpacemeshDataFolders(t)

}
