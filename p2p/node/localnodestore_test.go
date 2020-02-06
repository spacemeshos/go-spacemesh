package node

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestNodeLocalStore(t *testing.T) {
	// start clean

	node, err := NewNodeIdentity()
	require.NoError(t, err, "failed to create new local node")

	temppath := os.TempDir() + "/" + uuid.New().String() + "_" + t.Name() + "/"
	err = node.PersistData(temppath)
	require.NoError(t, err, "failed to persist node data")

	readNode, err := readNodeData(temppath, node.publicKey.String())
	require.NoError(t, err, "failed to read node from file")

	rnode, nerr := newLocalNodeFromFile(readNode)
	require.NoError(t, nerr, "failed to parse node keys")
	require.Equal(t, rnode.publicKey, node.publicKey)

}
