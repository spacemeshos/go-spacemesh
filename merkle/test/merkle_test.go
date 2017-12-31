package test

import (
	"bytes"
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle"
	"path/filepath"
	"testing"
)

func TestEmptyTreeCreation(t *testing.T) {

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

	err = m.CloseDataStores()
	assert.NoErr(t, err, "failed to close data stores")

}

func TestSimpleTreeOps(t *testing.T) {

	tempDir, err := filesystem.GetSpaceMeshTempDirectoryPath()
	assert.NoErr(t, err, "failed to get temp dir")

	userDb := filepath.Join(tempDir, "userdata.db")
	treeDb := filepath.Join(tempDir, "tree.db")

	m, err := merkle.NewEmptyTree(userDb, treeDb)
	assert.NoErr(t, err, "failed to create new merkle tree")

	// k,v can be any bytes
	v := []byte("this is some user data bytes")
	k := []byte("this is a user-space provided key to the value")

	log.Info("User key hex: %s", hex.EncodeToString(k))

	err = m.Put(k, v)
	assert.NoErr(t, err, "failed to put data")

	root := m.GetRootNode()
	assert.NotNil(t, root, "expected non-empty tree")

	data, ok, err := m.Get(k)
	assert.True(t, ok, "expected data from m tree for this key")
	assert.NoErr(t, err, "failed to get data")

	assert.True(t, bytes.Equal(data, v), "unexpected data")

	err = m.CloseDataStores()
	assert.NoErr(t, err, "failed to close m data stores")

	/////////////////////////

	// restore tree to a new instance based on root hash
	rootHash := m.GetRootHash()
	m1, err := merkle.NewTreeFromDb(rootHash, userDb, treeDb)

	root = m1.GetRootNode()
	assert.NotNil(t, root, "expected non-empty tree")

	rootHash1 := m1.GetRootHash()

	assert.True(t, bytes.Equal(rootHash, rootHash1), "expected same root hash")

	// test getting the data from the new tree instance
	data, ok, err = m1.Get(k)
	assert.True(t, ok, "expected data from tree")
	assert.NoErr(t, err, "failed to get data")
	assert.True(t, bytes.Equal(data, v), "unexpected data")

	err = m.CloseDataStores()
	assert.NoErr(t, err, "failed to close data stores")

	err = m1.CloseDataStores()
	assert.NoErr(t, err, "failed to close data stores")

}
