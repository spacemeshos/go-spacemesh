package activation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const layersPerEpochBig = 1000

func getAtxDb(tb testing.TB, id string) *DB {
	lg := logtest.New(tb).WithName(id)
	return NewDB(sql.InMemory(), nil, layersPerEpochBig, goldenATXID, &ValidatorMock{}, lg)
}

func processAtxs(db *DB, atxs []*types.ActivationTx) error {
	for _, atx := range atxs {
		err := db.ProcessAtx(context.TODO(), atx)
		if err != nil {
			return fmt.Errorf("process ATX: %w", err)
		}
	}
	return nil
}

func TestActivationDb_GetNodeLastAtxId(t *testing.T) {
	r := require.New(t)

	atxdb := getAtxDb(t, "t6")
	id1 := types.NodeID{1}
	coinbase1 := types.HexToAddress("aaaa")
	epoch1 := types.EpochID(2)
	atx1 := types.NewActivationTx(newChallenge(id1, 0, *types.EmptyATXID, goldenATXID, epoch1.FirstLayer()), coinbase1, &types.NIPost{}, 0, nil)
	r.NoError(atxdb.StoreAtx(context.TODO(), epoch1, atx1))

	epoch2 := types.EpochID(1) + (1 << 8)
	// This will fail if we convert the epoch id to bytes using LittleEndian, since LevelDB's lexicographic sorting will
	// then sort by LSB instead of MSB, first.
	atx2 := types.NewActivationTx(newChallenge(id1, 1, atx1.ID(), atx1.ID(), epoch2.FirstLayer()), coinbase1, &types.NIPost{}, 0, nil)
	r.NoError(atxdb.StoreAtx(context.TODO(), epoch2, atx2))

	id, err := atxdb.GetNodeLastAtxID(id1)
	r.NoError(err)
	r.Equal(atx2.ShortString(), id.ShortString(), "atx1.ShortString(): %v", atx1.ShortString())
}

func TestMesh_processBlockATXs(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpochBig)
	atxdb := getAtxDb(t, "t6")

	id1 := types.NodeID{1}
	id2 := types.NodeID{2}
	id3 := types.NodeID{3}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	npst := NewNIPostWithChallenge(&chlng, poetRef)
	posATX := newActivationTx(types.NodeID{3, 3}, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1000), 0, 100, coinbase1, 100, npst)
	err := atxdb.StoreAtx(context.TODO(), 0, posATX)
	assert.NoError(t, err)
	atxList := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, posATX.ID(), types.NewLayerID(1012), 0, 100, coinbase1, 100, &types.NIPost{}),
		newActivationTx(id2, 0, *types.EmptyATXID, posATX.ID(), types.NewLayerID(1300), 0, 100, coinbase2, 100, &types.NIPost{}),
		newActivationTx(id3, 0, *types.EmptyATXID, posATX.ID(), types.NewLayerID(1435), 0, 100, coinbase3, 100, &types.NIPost{}),
	}
	for _, atx := range atxList {
		hash, err := atx.NIPostChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPost = NewNIPostWithChallenge(hash, poetRef)
	}

	err = processAtxs(atxdb, atxList)
	assert.NoError(t, err)

	// check that further atxList dont affect current epoch count
	atxList2 := []*types.ActivationTx{
		newActivationTx(id1, 1, atxList[0].ID(), atxList[0].ID(), types.NewLayerID(2012), 0, 100, coinbase1, 100, &types.NIPost{}),
		newActivationTx(id2, 1, atxList[1].ID(), atxList[1].ID(), types.NewLayerID(2300), 0, 100, coinbase2, 100, &types.NIPost{}),
		newActivationTx(id3, 1, atxList[2].ID(), atxList[2].ID(), types.NewLayerID(2435), 0, 100, coinbase3, 100, &types.NIPost{}),
	}
	for _, atx := range atxList2 {
		hash, err := atx.NIPostChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPost = NewNIPostWithChallenge(hash, poetRef)
	}
	err = processAtxs(atxdb, atxList2)
	assert.NoError(t, err)

	assertEpochWeight(t, atxdb, 2, 100*100*4) // 1 posATX + 3 from `atxList`
	assertEpochWeight(t, atxdb, 3, 100*100*3) // 3 from `atxList2`
}

func assertEpochWeight(t *testing.T, atxdb *DB, epochID types.EpochID, expectedWeight uint64) {
	epochWeight, _, err := atxdb.GetEpochWeight(epochID)
	assert.NoError(t, err)
	assert.Equal(t, int(expectedWeight), int(epochWeight),
		fmt.Sprintf("expectedWeight (%d) != epochWeight (%d)", expectedWeight, epochWeight))
}

func TestActivationDB_ValidateAtx(t *testing.T) {
	atxdb := getAtxDb(t, "t8")

	signer := signing.NewEdSigner()
	idx1 := types.BytesToNodeID(signer.PublicKey().Bytes())

	id1 := types.NodeID{1}
	id2 := types.NodeID{2}
	id3 := types.NodeID{3}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase1, 100, &types.NIPost{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase2, 100, &types.NIPost{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase3, 100, &types.NIPost{}),
	}
	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxs {
		hash, err := atx.NIPostChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPost = NewNIPostWithChallenge(hash, poetRef)
	}

	prevAtx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase1, 100, &types.NIPost{})
	hash, err := prevAtx.NIPostChallenge.Hash()
	assert.NoError(t, err)
	prevAtx.NIPost = NewNIPostWithChallenge(hash, poetRef)

	atx := newActivationTx(idx1, 1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase1, 100, &types.NIPost{})
	hash, err = atx.NIPostChallenge.Hash()
	assert.NoError(t, err)
	atx.NIPost = NewNIPostWithChallenge(hash, poetRef)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)

	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.NoError(t, err)

	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.NoError(t, err)
}

func TestActivationDB_ValidateAtxErrors(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpochBig)

	atxdb := getAtxDb(t, "t8")
	signer := signing.NewEdSigner()
	idx1 := types.NodeID{Key: signer.PublicKey().String()}
	idx2 := types.NodeID{Key: uuid.New().String()}
	coinbase := types.HexToAddress("aaaa")

	id1 := types.NodeID{Key: uuid.New().String()}
	id2 := types.NodeID{Key: uuid.New().String()}
	id3 := types.NodeID{Key: uuid.New().String()}
	atxList := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
	}
	require.NoError(t, processAtxs(atxdb, atxList))

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := NewNIPostWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	posAtx := newActivationTx(idx2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	err := atxdb.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(context.TODO(), 1, posAtx)
	assert.NoError(t, err)

	// Wrong sequence.
	atx := newActivationTx(idx1, 0, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	// Wrong active set.
	/*atx = newActivationTx(idx1, 1, prevAtx.ID(), posatx.ID(), types.NewLayerID(1012), 0, 100, 100, coinbase, 10, []types.BlockID{}, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx contains view with unequal weight (10) than seen (0)")
	*/
	// Wrong positioning atx.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), atxList[0].ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "expected distance of one epoch (1000 layers) from pos atx but found 1011")

	// Empty positioning atx.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), *types.EmptyATXID, types.NewLayerID(2000), 0, 1, coinbase, 3, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "empty positioning atx")

	// Using Golden ATX in epochs other than 1 is not allowed. Testing epoch 0.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, goldenATXID, types.NewLayerID(0), 0, 1, coinbase, 3, &types.NIPost{PostMetadata: &types.PostMetadata{}})
	atx.InitialPost = initialPost
	atx.InitialPostIndices = initialPost.Indices
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "golden atx used for atx in epoch 0, but is only valid in epoch 1")

	// Using Golden ATX in epochs other than 1 is not allowed. Testing epoch 2.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), goldenATXID, types.NewLayerID(2000), 0, 1, coinbase, 3, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "golden atx used for atx in epoch 2, but is only valid in epoch 1")

	// Wrong prevATx.
	atx = newActivationTx(idx1, 1, atxList[0].ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, fmt.Sprintf("previous atx belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v", atx.ShortString(), atx.NodeID.Key, atxList[0].NodeID.Key))

	// Wrong layerId.
	posAtx2 := newActivationTx(idx2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1020), 0, 100, coinbase, 100, npst)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(context.TODO(), 1, posAtx2)
	assert.NoError(t, err)
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx2.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, npst)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "atx layer (1012) must be after positioning atx layer (1020)")

	// Atx already exists.
	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(12), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	// Prev atx declared but not found.
	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(12), 0, 100, coinbase, 100, &types.NIPost{})
	require.NoError(t, atxs.DeleteATXsByNodeID(atxdb.sqlDB, atx.NodeID))

	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err,
		fmt.Sprintf("could not fetch node last atx: find ATX in DB: atx for node %v does not exist", atx.NodeID.ShortString()))

	// Prev atx not declared but initial Post not included.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "no prevATX declared, but initial Post is not included")

	// Prev atx not declared but initial Post indices not included.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPost = initialPost
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "no prevATX declared, but initial Post indices is not included in challenge")

	// Challenge and initial Post indices mismatch.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPost = initialPost
	atx.InitialPostIndices = append([]byte{}, initialPost.Indices...)
	atx.InitialPostIndices[0]++
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "initial Post indices included in challenge does not equal to the initial Post indices included in the atx")

	// Prev atx declared but initial Post is included.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPost = initialPost
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "prevATX declared, but initial Post is included")

	// Prev atx declared but initial Post indices is included.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPostIndices = initialPost.Indices
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "prevATX declared, but initial Post indices is included in challenge")

	// Prev atx has publication layer in the same epoch as the atx.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(100), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "prevAtx epoch (0, layer 100) isn't older than current atx epoch (0, layer 100)")

	// NodeID and etracted pubkey dont match
	atx = newActivationTx(idx2, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 1, &types.NIPost{})
	atx.InitialPost = initialPost
	atx.InitialPostIndices = append([]byte{}, initialPost.Indices...)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "node ids don't match")
}

func TestActivationDB_ValidateAndInsertSorted(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	atxdb := getAtxDb(t, "t8")
	signer := signing.NewEdSigner()
	idx1 := types.NodeID{Key: signer.PublicKey().String()}
	coinbase := types.HexToAddress("aaaa")

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := NewNIPostWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)

	err := atxdb.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)

	// wrong sequence
	atx := newActivationTx(idx1, 1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(idx1, 2, atx.ID(), atx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	assert.NoError(t, err)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atx2id := atx.ID()

	atx = newActivationTx(idx1, 4, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)

	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(idx1, 3, atx2id, prevAtx.ID(), types.NewLayerID(1012+layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	id, err := atxdb.GetNodeLastAtxID(idx1)
	assert.NoError(t, err)
	assert.Equal(t, atx.ID(), id)

	_, err = atxdb.GetAtxHeader(id)
	assert.NoError(t, err)

	_, err = atxdb.GetAtxHeader(atx2id)
	assert.NoError(t, err)

	// test same sequence
	idx2 := types.NodeID{Key: uuid.New().String()}

	prevAtx = newActivationTx(idx2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	err = atxdb.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)
	atx = newActivationTx(idx2, 1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atxID := atx.ID()

	atx = newActivationTx(idx2, 2, atxID, atx.ID(), types.NewLayerID(1012+layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(idx2, 2, atxID, atx.ID(), types.NewLayerID(1012+2*layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
}

func TestActivationDb_ProcessAtx(t *testing.T) {
	r := require.New(t)

	atxdb := getAtxDb(t, "t8")
	idx1 := types.NodeID{Key: uuid.New().String()}
	coinbase := types.HexToAddress("aaaa")
	atx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, &types.NIPost{})

	err := atxdb.ProcessAtx(context.TODO(), atx)
	r.NoError(err)
	r.NoError(err)
}

func BenchmarkActivationDb_SyntacticallyValidateAtx(b *testing.B) {
	r := require.New(b)
	atxdb := getAtxDb(b, "t8")

	const (
		activesetSize         = 300
		blocksPerLayer        = 200
		numberOfLayers uint32 = 100
	)

	coinbase := types.HexToAddress("c012ba5e")
	var atxList []*types.ActivationTx
	for i := 0; i < activesetSize; i++ {
		id := types.NodeID{Key: uuid.New().String()}
		atxList = append(atxList, newActivationTx(id, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}))
	}

	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxList {
		hash, err := atx.NIPostChallenge.Hash()
		r.NoError(err)
		atx.NIPost = NewNIPostWithChallenge(hash, poetRef)
	}

	idx1 := types.NodeID{Key: uuid.New().String()}
	challenge := newChallenge(idx1, 0, *types.EmptyATXID, goldenATXID, types.LayerID{}.Add(numberOfLayers+1))
	hash, err := challenge.Hash()
	r.NoError(err)
	prevAtx := newAtx(challenge, NewNIPostWithChallenge(hash, poetRef))

	atx := newActivationTx(idx1, 1, prevAtx.ID(), prevAtx.ID(), types.LayerID{}.Add(numberOfLayers+1+layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
	hash, err = atx.NIPostChallenge.Hash()
	r.NoError(err)
	atx.NIPost = NewNIPostWithChallenge(hash, poetRef)
	err = atxdb.StoreAtx(context.TODO(), 1, prevAtx)
	r.NoError(err)

	start := time.Now()
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	b.Logf("\nSyntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxdb.SyntacticallyValidateAtx(context.TODO(), atx)
	b.Logf("\nSecond syntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	b.Logf("\nContextual validation took %v\n\n", time.Since(start))
	r.NoError(err)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)

	const tmpPath = "../tmp/atx"
	lg := logtest.New(b)

	atxdb := NewDB(sql.InMemory(), nil, layersPerEpochBig, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB"))

	const (
		numOfMiners = 300
		batchSize   = 15
		numOfEpochs = 10 * batchSize
	)
	prevAtxs := make([]types.ATXID, numOfMiners)
	pPrevAtxs := make([]types.ATXID, numOfMiners)
	posAtx := prevAtxID
	var atx *types.ActivationTx
	layer := postGenesisEpochLayer

	start := time.Now()
	eStart := time.Now()
	for epoch := postGenesisEpoch; epoch < postGenesisEpoch+numOfEpochs; epoch++ {
		for miner := 0; miner < numOfMiners; miner++ {
			challenge := newChallenge(nodeID, 1, prevAtxs[miner], posAtx, layer)
			h, err := challenge.Hash()
			r.NoError(err)
			atx = newAtx(challenge, NewNIPostWithChallenge(h, poetBytes))
			prevAtxs[miner] = atx.ID()
			storeAtx(r, atxdb, atx, lg.WithName("storeAtx"))
		}
		// noinspection GoNilness
		posAtx = atx.ID()
		layer = layer.Add(layersPerEpoch)
		if epoch%batchSize == batchSize-1 {
			b.Logf("epoch %3d-%3d took %v\t", epoch-(batchSize-1), epoch, time.Since(eStart))
			eStart = time.Now()

			for miner := 0; miner < numOfMiners; miner++ {
				atx, err := atxdb.GetAtxHeader(prevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
				atx, err = atxdb.GetAtxHeader(pPrevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
			}
			b.Logf("reading last and previous epoch 100 times took %v\n", time.Since(eStart))
			eStart = time.Now()
		}
		copy(pPrevAtxs, prevAtxs)
	}
	b.Logf("\n>>> Total time: %v\n\n", time.Since(start))
	time.Sleep(1 * time.Second)
}

func TestActivationDb_TopAtx(t *testing.T) {
	r := require.New(t)

	atxdb := getAtxDb(t, "t8")

	// ATX stored should become top ATX
	atx, err := createAndStoreAtx(atxdb, types.LayerID{}.Add(1))
	r.NoError(err)

	id, err := atxdb.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx.ID(), id)

	// higher-layer ATX stored should become new top ATX
	atx, err = createAndStoreAtx(atxdb, types.LayerID{}.Add(3))
	r.NoError(err)

	id, err = atxdb.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx.ID(), id)

	// lower-layer ATX stored should NOT become new top ATX
	atx, err = createAndStoreAtx(atxdb, types.LayerID{}.Add(1))
	r.NoError(err)

	id, err = atxdb.GetPosAtxID()
	r.NoError(err)
	r.NotEqual(atx.ID(), id)
}

func createAndValidateSignedATX(r *require.Assertions, atxdb *DB, ed *signing.EdSigner, atx *types.ActivationTx) (*types.ActivationTx, error) {
	atxBytes, err := types.InterfaceToBytes(atx.InnerActivationTx)
	r.NoError(err)
	sig := ed.Sign(atxBytes)

	signedAtx := &types.ActivationTx{InnerActivationTx: atx.InnerActivationTx, Sig: sig}
	return signedAtx, atxdb.ValidateSignedAtx(*ed.PublicKey(), signedAtx)
}

func TestActivationDb_ValidateSignedAtx(t *testing.T) {
	r := require.New(t)
	lg := logtest.New(t).WithName("sigValidation")
	atxdb := NewDB(sql.InMemory(), nil, layersPerEpochBig, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB"))

	ed := signing.NewEdSigner()
	nodeID := types.NodeID{Key: ed.PublicKey().String()}

	// test happy flow of first ATX
	emptyAtx := types.EmptyATXID
	atx := newActivationTx(nodeID, 1, *emptyAtx, *emptyAtx, types.LayerID{}.Add(15), 1, 100, coinbase, 100, nipost)
	_, err := createAndValidateSignedATX(r, atxdb, ed, atx)
	r.NoError(err)

	// test negative flow no atx found in idstore
	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	atx = newActivationTx(nodeID, 1, prevAtx, prevAtx, types.LayerID{}.Add(15), 1, 100, coinbase, 100, nipost)
	signedAtx, err := createAndValidateSignedATX(r, atxdb, ed, atx)
	r.Equal(errInvalidSig, err)

	// test happy flow not first ATX
	_, err = createAndValidateSignedATX(r, atxdb, ed, atx)
	r.NoError(err)

	// test negative flow not first ATX, invalid sig
	signedAtx.Sig = []byte("anton")
	_, err = ExtractPublicKey(signedAtx)
	r.Error(err)
}

func createAndStoreAtx(atxdb *DB, layer types.LayerID) (*types.ActivationTx, error) {
	id := types.NodeID{Key: uuid.New().String()}
	atx := newActivationTx(id, 0, *types.EmptyATXID, *types.EmptyATXID, layer, 0, 100, coinbase, 100, &types.NIPost{})
	err := atxdb.StoreAtx(context.TODO(), atx.TargetEpoch(), atx)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func TestActivationDb_AwaitAtx(t *testing.T) {
	r := require.New(t)

	lg := logtest.New(t).WithName("sigValidation")
	atxdb := NewDB(sql.InMemory(), nil, layersPerEpochBig, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB"))
	id := types.NodeID{Key: uuid.New().String()}
	atx := newActivationTx(id, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{})

	ch := atxdb.AwaitAtx(atx.ID())
	r.Len(atxdb.atxChannels, 1) // channel was created

	select {
	case <-ch:
		r.Fail("notified before ATX was stored")
	default:
	}

	err := atxdb.StoreAtx(context.TODO(), atx.TargetEpoch(), atx)
	r.NoError(err)
	r.Len(atxdb.atxChannels, 0) // after notifying subscribers, channel is cleared

	select {
	case <-ch:
	default:
		r.Fail("not notified after ATX was stored")
	}

	otherID := types.ATXID{}
	copy(otherID[:], "abcd")
	atxdb.AwaitAtx(otherID)
	r.Len(atxdb.atxChannels, 1) // after first subscription - channel is created
	atxdb.AwaitAtx(otherID)
	r.Len(atxdb.atxChannels, 1) // second subscription to same id - no additional channel
	atxdb.UnsubscribeAtx(otherID)
	r.Len(atxdb.atxChannels, 1) // first unsubscribe doesn't clear the channel
	atxdb.UnsubscribeAtx(otherID)
	r.Len(atxdb.atxChannels, 0) // last unsubscribe clears the channel
}

func TestActivationDb_ContextuallyValidateAtx(t *testing.T) {
	r := require.New(t)

	lg := logtest.New(t).WithName("sigValidation")
	atxdb := NewDB(sql.InMemory(), nil, layersPerEpochBig, goldenATXID, &ValidatorMock{}, lg.WithName("atxDB"))

	validAtx := types.NewActivationTx(newChallenge(nodeID, 0, *types.EmptyATXID, goldenATXID, types.LayerID{}), types.Address{}, nil, 0, nil)
	err := atxdb.ContextuallyValidateAtx(validAtx.ActivationTxHeader)
	r.NoError(err)

	arbitraryAtxID := types.ATXID(types.HexToHash32("11111"))
	malformedAtx := types.NewActivationTx(newChallenge(nodeID, 0, arbitraryAtxID, goldenATXID, types.LayerID{}), types.Address{}, nil, 0, nil)
	err = atxdb.ContextuallyValidateAtx(malformedAtx.ActivationTxHeader)
	r.EqualError(err,
		fmt.Sprintf("could not fetch node last atx: find ATX in DB: atx for node %v does not exist", nodeID.ShortString()))
}

func TestActivationDB_HandleAtxNilNipst(t *testing.T) {
	atxdb := getAtxDb(t, t.Name())
	atx := newActivationTx(nodeID, 0, *types.EmptyATXID, *types.EmptyATXID, types.LayerID{}, 0, 0, coinbase, 0, nil)
	buf, err := types.InterfaceToBytes(atx)
	require.NoError(t, err)
	require.Error(t, atxdb.HandleAtxData(context.TODO(), buf))
}

func TestActivationDB_KnownATX(t *testing.T) {
	atxdb := getAtxDb(t, t.Name())
	atx := newActivationTx(nodeID, 0, *types.EmptyATXID, *types.EmptyATXID, types.LayerID{}, 0, 0, coinbase, 0, nil)
	require.NoError(t, atxdb.ProcessAtx(context.TODO(), atx))
	buf, err := codec.Encode(atx)
	require.NoError(t, err)

	require.NoError(t, atxdb.HandleAtxData(context.TODO(), buf))
	require.Equal(t, pubsub.ValidationIgnore, atxdb.HandleGossipAtx(context.TODO(), "", buf))
}

func BenchmarkGetAtxHeaderWithConcurrentStoreAtx(b *testing.B) {
	lg := logtest.New(b)
	atxdb := NewDB(sql.InMemory(), nil, 288, types.ATXID{}, &Validator{}, lg)

	var (
		stop uint64
		wg   sync.WaitGroup
	)
	for i := 0; i < runtime.NumCPU()/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pub, _, _ := ed25519.GenerateKey(nil)
			id := types.NodeID{Key: util.Bytes2Hex(pub)}
			for i := 0; ; i++ {
				atx := types.NewActivationTx(newChallenge(id, uint64(i), *types.EmptyATXID, goldenATXID, types.NewLayerID(0)), types.Address{}, nil, 0, nil)
				if !assert.NoError(b, atxdb.StoreAtx(context.TODO(), types.EpochID(1), atx)) {
					return
				}
				if atomic.LoadUint64(&stop) == 1 {
					return
				}
			}
		}()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := atxdb.GetAtxHeader(types.ATXID{1, 1, 1})
		require.ErrorIs(b, err, database.ErrNotFound)
	}
	atomic.StoreUint64(&stop, 1)
	wg.Wait()
}
