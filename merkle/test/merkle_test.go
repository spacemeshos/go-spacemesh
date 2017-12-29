package test

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/merkle"
	"path/filepath"
	"testing"
)

func TestTreeCreation(t *testing.T) {

	tempDir, err := filesystem.GetSpaceMeshTempDirectoryPath()
	assert.NoErr(t, err, "failed to get temp dir")

	userDb := filepath.Join(tempDir, "userdata.db")
	treeDb := filepath.Join(tempDir, "tree.db")

	m, err := merkle.NewEmptyTree(userDb, treeDb)
	assert.NoErr(t, err, "failed to create new merkle tree")

	root := m.GetRootNode()
	assert.Nil(t, root, "expected empty tree")

	hash := m.GetRootHash()
	assert.True(t, bytes.Equal(merkle.EmptyTreeRootHash, hash), "unexpected empty tree root hash")
}
