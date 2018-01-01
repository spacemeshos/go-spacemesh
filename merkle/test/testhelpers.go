package test

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/merkle"
	"path/filepath"
	"testing"
)

// Various helper methods for testing merkle tree ops

// helper method - validate we can get value from tree and that result matches expected value v
func validateGet(t *testing.T, tree merkle.MerkleTree, k, v []byte) {
	res, ok, err := tree.Get(k)
	assert.True(t, ok, "expected data from tree")
	assert.NoErr(t, err, "failed to get data")
	assert.True(t, bytes.Equal(res, v), "unexpected data")
}

func tryPut(t *testing.T, tree merkle.MerkleTree, k, v []byte) {
	err := tree.Put(k, v)
	assert.NoErr(t, err, "failed to put data.")
}

func getDbPaths(t *testing.T) (string, string) {
	tempDir, err := filesystem.GetSpaceMeshTempDirectoryPath()
	assert.NoErr(t, err, "failed to get temp dir")
	userDb := filepath.Join(tempDir, "userdata.db")
	treeDb := filepath.Join(tempDir, "tree.db")
	return userDb, treeDb
}
