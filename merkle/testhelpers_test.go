package merkle

import (
	"bytes"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

// Various helper methods for testing merkle tree ops

// helper method - validate we can get value from tree and that result matches expected value v
func validateGet(t *testing.T, tree Tree, k, v []byte) {
	t.Helper()
	res, _, err := tree.Get(k)
	assert.NoError(t, err, "failed to get data")
	assert.True(t, bytes.Equal(res, v), "unexpected data")
}

func tryPut(t *testing.T, tree Tree, k, v []byte) {
	t.Helper()
	err := tree.Put(k, v)
	assert.NoError(t, err, "failed to put data.")
}

func getDbPaths(t *testing.T) (string, string) {
	t.Helper()
	tempDir := os.TempDir() + uuid.New().String() + "/" + t.Name()
	err := os.MkdirAll(tempDir, os.ModeDir)
	require.NoError(t, err)
	userDb := filepath.Join(tempDir, "userdata.db")
	treeDb := filepath.Join(tempDir, "tree.db")
	return userDb, treeDb
}
