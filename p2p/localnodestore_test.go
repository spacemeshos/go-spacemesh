package p2p

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
	"time"
)

func TestNodeLocalStore(t *testing.T) {

	// start clean
	filesystem.DeleteSpacemeshDataFolders(t)

	_, err := ensureNodesDataDirectory()
	assert.NoErr(t, err, "failed to create or verify nodes data dir")

	port1 := crypto.GetRandomUserPort()
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

	data, err := readNodeData(node.String())
	assert.NoErr(t, err, "failed to ensure node data directory")
	assert.NotNil(t, data, "expected node data")
	assert.Equal(t, data.PubKey, node.String(), "expected same node id")

	// as we deleted all dirs - first node data in nodes folder should be this  node's data
	data1, err := readFirstNodeData()
	assert.NoErr(t, err, "failed to ensure node data directory")
	assert.NotNil(t, data1, "expected node data")
	assert.Equal(t, data1.PubKey, node.String(), "expected same node id")

	// create a new local node from persisted node data
	node1, err := NewLocalNode(address, nodeconfig.ConfigValues, true)
	assert.NoErr(t, err, "local node creation error")
	assert.Equal(t, node.String(), node1.String(), "expected restored node")

	// cleanup
	filesystem.DeleteSpacemeshDataFolders(t)

}
