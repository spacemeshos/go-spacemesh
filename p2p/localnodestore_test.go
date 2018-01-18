package p2p

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
)

func TestNodeLocalStore(t *testing.T) {

	// start clean
	filesystem.DeleteSpaceMeshDataFolders(t)

	_, err := ensureNodesDataDirectory()
	assert.NoErr(t, err, "failed to create or verify nodes data dir")

	port1 := crypto.GetRandomUInt32(10000) + 1000
	address := fmt.Sprintf("localhost:%d", port1)

	node, err := NewNodeIdentity(address, nodeconfig.ConfigValues, false)
	assert.NoErr(t, err, "failed to create new local node")

	err = node.persistData()
	assert.NoErr(t, err, "failed to persist node data")

	_, err = node.ensureNodeDataDirectory()
	assert.NoErr(t, err, "failed to ensure node data directory")

	node.Shutdown()

	node1, err := NewLocalNode(address, nodeconfig.ConfigValues, true)
	assert.Equal(t, node.String(),node1.String(), "expected restored node")

}
