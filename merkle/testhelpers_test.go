package merkle

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"path/filepath"
	"testing"
)

// Various helper methods for testing merkle tree ops

// helper method - validate we can get value from tree and that result matches expected value v
func validateGet(t *testing.T, tree MerkleTree, k, v []byte) {
	t.Helper()
	res, _, err := tree.Get(k)
	assert.NoErr(t, err, "failed to get data")
	assert.True(t, bytes.Equal(res, v), "unexpected data")
}

func tryPut(t *testing.T, tree MerkleTree, k, v []byte) {
	t.Helper()
	err := tree.Put(k, v)
	assert.NoErr(t, err, "failed to put data.")
}

func getDbPaths(t *testing.T) (string, string) {
	t.Helper()
	tempDir, err := filesystem.GetSpaceMeshTempDirectoryPath()
	assert.NoErr(t, err, "failed to get temp dir")
	userDb := filepath.Join(tempDir, "userdata.db")
	treeDb := filepath.Join(tempDir, "tree.db")
	return userDb, treeDb
}
