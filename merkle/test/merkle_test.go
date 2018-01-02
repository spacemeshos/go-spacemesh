package test

import (
	"bytes"
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/merkle"
	"testing"
)

func TestEmptyTreeCreation(t *testing.T) {

	userDb, treeDb := getDbPaths(t)
	m, err := merkle.NewEmptyTree(userDb, treeDb)
	assert.NoErr(t, err, "failed to create new merkle tree")

	root := m.GetRootNode()
	assert.Nil(t, root, "expected empty tree")

	hash := m.GetRootHash()
	assert.True(t, bytes.Equal(merkle.EmptyTreeRootHash, hash), "unexpected empty tree root hash")

	err = m.CloseDataStores()
	assert.NoErr(t, err, "failed to close data stores")
}

// Test a simple 1-node merkle tree
func TestSimpleTreeOps(t *testing.T) {

	userDb, treeDb := getDbPaths(t)
	m, err := merkle.NewEmptyTree(userDb, treeDb)
	defer m.CloseDataStores() // we need to close the data stores when done w m - they are owned by m

	assert.NoErr(t, err, "failed to create new merkle tree")

	// user data k,v can be any bytes
	v := []byte("zifton-the-immortal")
	k := []byte("the-name-of-my-cat")

	log.Info("User key hex: %s", hex.EncodeToString(k))

	tryPut(t, m, k, v)

	root := m.GetRootNode()
	assert.NotNil(t, root, "expected non-empty tree")

	validateGet(t, m, k, v)

	err = m.CloseDataStores()
	assert.NoErr(t, err, "failed to close m data stores")

	/////////////////////////

	// restore tree to a new instance based on root hash
	rootHash := m.GetRootHash()
	m1, err := merkle.NewTreeFromDb(rootHash, userDb, treeDb)
	defer m1.CloseDataStores() // tell m1 to close data stores when we are done w it

	root = m1.GetRootNode()
	assert.NotNil(t, root, "expected non-empty tree")

	rootHash1 := m1.GetRootHash()

	assert.True(t, bytes.Equal(rootHash, rootHash1), "expected same root hash")

	// test getting the data from the new tree instance

	validateGet(t, m1, k, v)

}

// Test a simple 1-node merkle tree
func TestComplexTreeOps(t *testing.T) {

	k1, err := hex.DecodeString("123456000001")
	assert.NoErr(t, err, "invalid hex str")
	v1 := []byte("zifton")

	k2, err := hex.DecodeString("112456000001")
	assert.NoErr(t, err, "invalid hex str")
	v2 := []byte("tantalus")

	//k3, err := hex.DecodeString("112457000001")
	//assert.NoErr(t, err, "invalid hex str")
	//v3, err := crypto.GetRandomBytes(100)
	//assert.NoErr(t, err, "failed to get random data")

	//v4, err := crypto.GetRandomBytes(100)
	//k4 := crypto.Sha256([]byte("key-to-tanalus"))
	//assert.NoErr(t, err, "failed to get random data")

	userDb, treeDb := getDbPaths(t)
	m, err := merkle.NewEmptyTree(userDb, treeDb)
	defer m.CloseDataStores() // we need to close the data stores when done w m - they are owned by m
	assert.NoErr(t, err, "failed to create new Merkle tree")

	tryPut(t, m, k1, v1)

	t.Log(m.Print())

	//validateGet(t, m, k1, v1)

	tryPut(t, m, k2, v2)
	//tryPut(t, m, k3, v3)
	//tryPut(t, m, k4, v4)

	t.Log(m.Print())

	//validateGet(t, m, k1, v1)
	//validateGet(t, m, k2, v2)
	//validateGet(t, m, k3, v3)
	//validateGet(t, m, k4, v4)

}
