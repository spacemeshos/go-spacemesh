package activation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	amocks "github.com/spacemeshos/go-spacemesh/activation/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const layersPerEpochBig = 1000

func getATXHandler(tb testing.TB, cdb *datastore.CachedDB) *Handler {
	return NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, &ValidatorMock{}, logtest.New(tb))
}

func processAtxs(db *Handler, atxs []*types.ActivationTx) error {
	for _, atx := range atxs {
		err := db.ProcessAtx(context.TODO(), atx)
		if err != nil {
			return fmt.Errorf("process ATX: %w", err)
		}
	}
	return nil
}

func newNIPostWithChallenge(challenge *types.Hash32, poetRef []byte) *types.NIPost {
	return &types.NIPost{
		Challenge: challenge,
		Post: &types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: &types.PostMetadata{
			Challenge: poetRef,
		},
	}
}

func TestHandler_GetNodeLastAtxId(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := getATXHandler(t, cdb)
	coinbase1 := types.GenerateAddress([]byte("aaaa"))
	epoch1 := types.EpochID(1)
	atx1 := newAtx(newChallenge(0, *types.EmptyATXID, goldenATXID, epoch1.FirstLayer()), sig, &types.NIPost{}, 0, coinbase1)
	r.NoError(atxHdlr.StoreAtx(context.TODO(), epoch1, atx1))

	epoch2 := types.EpochID(2)
	atx2 := newAtx(newChallenge(1, atx1.ID(), atx1.ID(), epoch2.FirstLayer()), sig, &types.NIPost{}, 0, coinbase1)
	r.NoError(atxHdlr.StoreAtx(context.TODO(), epoch2, atx2))

	atxid, err := atxs.GetLastIDByNodeID(cdb, sig.NodeID())
	r.NoError(err)
	r.Equal(atx2.ID(), atxid, "atx2.ShortString(): %v", atx2.ShortString())
}

func TestMesh_processBlockATXs(t *testing.T) {
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() {
		types.SetLayersPerEpoch(layers)
	})

	cdb := newCachedDB(t)
	atxHdlr := getATXHandler(t, cdb)

	coinbase1 := types.GenerateAddress([]byte("aaaa"))
	coinbase2 := types.GenerateAddress([]byte("bbbb"))
	coinbase3 := types.GenerateAddress([]byte("cccc"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	npst := newNIPostWithChallenge(&chlng, poetRef)
	posATX := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1000), 0, 100, coinbase1, 100, npst)
	err := atxHdlr.StoreAtx(context.TODO(), 0, posATX)
	assert.NoError(t, err)
	atxList := []*types.ActivationTx{
		newActivationTx(sig, 0, *types.EmptyATXID, posATX.ID(), types.NewLayerID(1012), 0, 100, coinbase1, 100, &types.NIPost{}),
		newActivationTx(sig, 0, *types.EmptyATXID, posATX.ID(), types.NewLayerID(1300), 0, 100, coinbase2, 100, &types.NIPost{}),
		newActivationTx(sig, 0, *types.EmptyATXID, posATX.ID(), types.NewLayerID(1435), 0, 100, coinbase3, 100, &types.NIPost{}),
	}
	for _, atx := range atxList {
		hash, err := atx.NIPostChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	}

	err = processAtxs(atxHdlr, atxList)
	assert.NoError(t, err)

	// check that further atxList don't affect current epoch count
	atxList2 := []*types.ActivationTx{
		newActivationTx(sig, 1, atxList[0].ID(), atxList[0].ID(), types.NewLayerID(2012), 0, 100, coinbase1, 100, &types.NIPost{}),
		newActivationTx(sig, 1, atxList[1].ID(), atxList[1].ID(), types.NewLayerID(2300), 0, 100, coinbase2, 100, &types.NIPost{}),
		newActivationTx(sig, 1, atxList[2].ID(), atxList[2].ID(), types.NewLayerID(2435), 0, 100, coinbase3, 100, &types.NIPost{}),
	}
	for _, atx := range atxList2 {
		hash, err := atx.NIPostChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	}
	err = processAtxs(atxHdlr, atxList2)
	assert.NoError(t, err)

	assertEpochWeight(t, cdb, 2, 100*100*4) // 1 posATX + 3 from `atxList`
	assertEpochWeight(t, cdb, 3, 100*100*3) // 3 from `atxList2`
}

func assertEpochWeight(t *testing.T, cdb *datastore.CachedDB, epochID types.EpochID, expectedWeight uint64) {
	epochWeight, _, err := cdb.GetEpochWeight(epochID)
	assert.NoError(t, err)
	assert.Equal(t, int(expectedWeight), int(epochWeight),
		fmt.Sprintf("expectedWeight (%d) != epochWeight (%d)", expectedWeight, epochWeight))
}

func TestHandler_ValidateAtx(t *testing.T) {
	atxHdlr := getATXHandler(t, newCachedDB(t))

	sig1 := NewMockSigner()
	sig2 := NewMockSigner()
	sig3 := NewMockSigner()
	coinbase1 := types.GenerateAddress([]byte("aaaa"))
	coinbase2 := types.GenerateAddress([]byte("bbbb"))
	coinbase3 := types.GenerateAddress([]byte("cccc"))
	atxs := []*types.ActivationTx{
		newActivationTx(sig1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase1, 100, &types.NIPost{}),
		newActivationTx(sig2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase2, 100, &types.NIPost{}),
		newActivationTx(sig3, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase3, 100, &types.NIPost{}),
	}
	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxs {
		hash, err := atx.NIPostChallenge.Hash()
		assert.NoError(t, err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	}

	prevAtx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase1, 100, &types.NIPost{})
	hash, err := prevAtx.NIPostChallenge.Hash()
	assert.NoError(t, err)
	prevAtx.NIPost = newNIPostWithChallenge(hash, poetRef)

	atx := newActivationTx(sig, 1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase1, 100, &types.NIPost{})
	hash, err = atx.NIPostChallenge.Hash()
	assert.NoError(t, err)
	atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)

	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.NoError(t, err)

	err = atxHdlr.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.NoError(t, err)
}

func TestHandler_ValidateAtxErrors(t *testing.T) {
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() {
		types.SetLayersPerEpoch(layers)
	})

	cdb := newCachedDB(t)
	atxHdlr := getATXHandler(t, cdb)
	otherSig := NewMockSigner()
	coinbase := types.GenerateAddress([]byte("aaaa"))

	sig1 := NewMockSigner()
	sig2 := NewMockSigner()
	sig3 := NewMockSigner()
	atxList := []*types.ActivationTx{
		newActivationTx(sig1, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(sig2, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(sig3, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
	}
	require.NoError(t, processAtxs(atxHdlr, atxList))

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := newNIPostWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	posAtx := newActivationTx(otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	err := atxHdlr.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)
	err = atxHdlr.StoreAtx(context.TODO(), 1, posAtx)
	assert.NoError(t, err)

	// Wrong sequence.
	atx := newActivationTx(sig, 0, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	// Wrong positioning atx.
	atx = newActivationTx(sig, 1, prevAtx.ID(), atxList[0].ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "expected distance of one epoch (1000 layers) from pos atx but found 1011")

	// Empty positioning atx.
	atx = newActivationTx(sig, 1, prevAtx.ID(), *types.EmptyATXID, types.NewLayerID(2000), 0, 1, coinbase, 3, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "empty positioning atx")

	// Using Golden ATX in epochs other than 1 is not allowed. Testing epoch 0.
	atx = newActivationTx(sig, 0, *types.EmptyATXID, goldenATXID, types.NewLayerID(0), 0, 1, coinbase, 3, &types.NIPost{PostMetadata: &types.PostMetadata{}})
	atx.InitialPost = initialPost
	atx.InitialPostIndices = initialPost.Indices
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "golden atx used for atx in epoch 0, but is only valid in epoch 1")

	// Using Golden ATX in epochs other than 1 is not allowed. Testing epoch 2.
	atx = newActivationTx(sig, 1, prevAtx.ID(), goldenATXID, types.NewLayerID(2000), 0, 1, coinbase, 3, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "golden atx used for atx in epoch 2, but is only valid in epoch 1")

	// Wrong prevATx.
	atx = newActivationTx(sig, 1, atxList[0].ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, fmt.Sprintf("previous atx belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v", atx.ShortString(), atx.NodeID(), atxList[0].NodeID()))

	// Wrong layerId.
	posAtx2 := newActivationTx(otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1020), 0, 100, coinbase, 100, npst)
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.StoreAtx(context.TODO(), 1, posAtx2)
	assert.NoError(t, err)
	atx = newActivationTx(sig, 1, prevAtx.ID(), posAtx2.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, npst)
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "atx layer (1012) must be after positioning atx layer (1020)")

	// Atx already exists.
	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atx = newActivationTx(sig, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(12), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxHdlr.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	// Prev atx declared but not found.
	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atx = newActivationTx(sig, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(12), 0, 100, coinbase, 100, &types.NIPost{})
	require.NoError(t, atxs.DeleteATXsByNodeID(cdb, atx.NodeID()))

	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.ErrorIs(t, err, sql.ErrNotFound)

	// Prev atx not declared but initial Post not included.
	atx = newActivationTx(sig, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "no prevATX declared, but initial Post is not included")

	// Prev atx not declared but initial Post indices not included.
	atx = newActivationTx(sig, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPost = initialPost
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "no prevATX declared, but initial Post indices is not included in challenge")

	// Challenge and initial Post indices mismatch.
	atx = newActivationTx(sig, 0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPost = initialPost
	atx.InitialPostIndices = append([]byte{}, initialPost.Indices...)
	atx.InitialPostIndices[0]++
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "initial Post indices included in challenge does not equal to the initial Post indices included in the atx")

	// Prev atx declared but initial Post is included.
	atx = newActivationTx(sig, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPost = initialPost
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "prevATX declared, but initial Post is included")

	// Prev atx declared but initial Post indices is included.
	atx = newActivationTx(sig, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	atx.InitialPostIndices = initialPost.Indices
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "prevATX declared, but initial Post indices is included in challenge")

	// Prev atx has publication layer in the same epoch as the atx.
	atx = newActivationTx(sig, 1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(100), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "prevAtx epoch (0, layer 100) isn't older than current atx epoch (0, layer 100)")
}

func TestHandler_ValidateAndInsertSorted(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	cdb := newCachedDB(t)
	atxHdlr := getATXHandler(t, cdb)
	coinbase := types.GenerateAddress([]byte("aaaa"))

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := newNIPostWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)

	err := atxHdlr.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)

	// wrong sequence
	atx := newActivationTx(sig, 1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(sig, 2, atx.ID(), atx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	assert.NoError(t, err)
	err = SignAtx(sig, atx)
	assert.NoError(t, err)
	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atx2id := atx.ID()

	atx = newActivationTx(sig, 4, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = SignAtx(sig, atx)
	assert.NoError(t, err)

	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(sig, 3, atx2id, prevAtx.ID(), types.NewLayerID(1012+layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxHdlr.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	id, err := atxs.GetLastIDByNodeID(cdb, sig.NodeID())
	assert.NoError(t, err)
	assert.Equal(t, atx.ID(), id)

	_, err = atxHdlr.cdb.GetAtxHeader(id)
	assert.NoError(t, err)

	_, err = atxHdlr.cdb.GetAtxHeader(atx2id)
	assert.NoError(t, err)

	// test same sequence
	otherSig := NewMockSigner()

	prevAtx = newActivationTx(otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	err = atxHdlr.StoreAtx(context.TODO(), 1, prevAtx)
	assert.NoError(t, err)
	atx = newActivationTx(otherSig, 1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
	atxID := atx.ID()

	atx = newActivationTx(otherSig, 2, atxID, atx.ID(), types.NewLayerID(1012+layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(otherSig, 2, atxID, atx.ID(), types.NewLayerID(1012+2*layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	err = atxHdlr.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxHdlr.StoreAtx(context.TODO(), 1, atx)
	assert.NoError(t, err)
}

func TestHandler_ProcessAtx(t *testing.T) {
	r := require.New(t)

	atxHdlr := getATXHandler(t, newCachedDB(t))
	coinbase := types.GenerateAddress([]byte("aaaa"))
	atx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(100), 0, 100, coinbase, 100, &types.NIPost{})

	err := atxHdlr.ProcessAtx(context.TODO(), atx)
	r.NoError(err)
}

func BenchmarkActivationDb_SyntacticallyValidateAtx(b *testing.B) {
	r := require.New(b)
	atxHdlr := getATXHandler(b, newCachedDB(b))

	const (
		activesetSize         = 300
		numberOfLayers uint32 = 100
	)

	coinbase := types.GenerateAddress([]byte("c012ba5e"))
	var atxList []*types.ActivationTx
	for i := 0; i < activesetSize; i++ {
		atxList = append(atxList, newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}))
	}

	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxList {
		hash, err := atx.NIPostChallenge.Hash()
		r.NoError(err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	}

	challenge := newChallenge(0, *types.EmptyATXID, goldenATXID, types.LayerID{}.Add(numberOfLayers+1))
	hash, err := challenge.Hash()
	r.NoError(err)
	prevAtx := newAtx(challenge, sig, newNIPostWithChallenge(hash, poetRef), 2, coinbase)

	atx := newActivationTx(sig, 1, prevAtx.ID(), prevAtx.ID(), types.LayerID{}.Add(numberOfLayers+1+layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
	hash, err = atx.NIPostChallenge.Hash()
	r.NoError(err)
	atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	err = atxHdlr.StoreAtx(context.TODO(), 1, prevAtx)
	r.NoError(err)

	start := time.Now()
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	b.Logf("\nSyntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	b.Logf("\nSecond syntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxHdlr.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	b.Logf("\nContextual validation took %v\n\n", time.Since(start))
	r.NoError(err)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)

	lg := logtest.New(b)

	cdb := newCachedDB(b)
	atxHdlr := NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, &ValidatorMock{}, lg.WithName("atxHandler"))

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
			challenge := newChallenge(1, prevAtxs[miner], posAtx, layer)
			h, err := challenge.Hash()
			r.NoError(err)
			atx = newAtx(challenge, sig, newNIPostWithChallenge(h, poetBytes), 2, coinbase)
			prevAtxs[miner] = atx.ID()
			storeAtx(r, atxHdlr, atx, lg.WithName("storeAtx"))
		}
		// noinspection GoNilness
		posAtx = atx.ID()
		layer = layer.Add(layersPerEpoch)
		if epoch%batchSize == batchSize-1 {
			b.Logf("epoch %3d-%3d took %v\t", epoch-(batchSize-1), epoch, time.Since(eStart))
			eStart = time.Now()

			for miner := 0; miner < numOfMiners; miner++ {
				atx, err := cdb.GetAtxHeader(prevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
				atx, err = cdb.GetAtxHeader(pPrevAtxs[miner])
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

func TestHandler_TopAtx(t *testing.T) {
	r := require.New(t)

	atxHdlr := getATXHandler(t, newCachedDB(t))

	// ATX stored should become top ATX
	atx, err := createAndStoreAtx(atxHdlr, types.LayerID{}.Add(1))
	r.NoError(err)

	id, err := atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx.ID(), id)

	// higher-layer ATX stored should become new top ATX
	atx, err = createAndStoreAtx(atxHdlr, types.LayerID{}.Add(layersPerEpochBig))
	r.NoError(err)

	id, err = atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx.ID(), id)

	// lower-layer ATX stored should NOT become new top ATX
	atx, err = createAndStoreAtx(atxHdlr, types.LayerID{}.Add(1))
	r.NoError(err)

	id, err = atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.NotEqual(atx.ID(), id)
}

func createAndStoreAtx(atxHdlr *Handler, layer types.LayerID) (*types.ActivationTx, error) {
	atx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, layer, 0, 100, coinbase, 100, &types.NIPost{})
	err := atxHdlr.StoreAtx(context.TODO(), atx.TargetEpoch(), atx)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func TestHandler_AwaitAtx(t *testing.T) {
	r := require.New(t)

	lg := logtest.New(t).WithName("sigValidation")
	atxHdlr := NewHandler(datastore.NewCachedDB(sql.InMemory(), lg), nil, layersPerEpochBig, testTickSize, goldenATXID, &ValidatorMock{}, lg.WithName("atxHandler"))
	atx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{})

	ch := atxHdlr.AwaitAtx(atx.ID())
	r.Len(atxHdlr.atxChannels, 1) // channel was created

	select {
	case <-ch:
		r.Fail("notified before ATX was stored")
	default:
	}

	err := atxHdlr.StoreAtx(context.TODO(), atx.TargetEpoch(), atx)
	r.NoError(err)
	r.Len(atxHdlr.atxChannels, 0) // after notifying subscribers, channel is cleared

	select {
	case <-ch:
	default:
		r.Fail("not notified after ATX was stored")
	}

	otherID := types.ATXID{}
	copy(otherID[:], "abcd")
	atxHdlr.AwaitAtx(otherID)
	r.Len(atxHdlr.atxChannels, 1) // after first subscription - channel is created
	atxHdlr.AwaitAtx(otherID)
	r.Len(atxHdlr.atxChannels, 1) // second subscription to same id - no additional channel
	atxHdlr.UnsubscribeAtx(otherID)
	r.Len(atxHdlr.atxChannels, 1) // first unsubscribe doesn't clear the channel
	atxHdlr.UnsubscribeAtx(otherID)
	r.Len(atxHdlr.atxChannels, 0) // last unsubscribe clears the channel
}

func TestHandler_ContextuallyValidateAtx(t *testing.T) {
	r := require.New(t)

	lg := logtest.New(t).WithName("sigValidation")
	atxHdlr := NewHandler(datastore.NewCachedDB(sql.InMemory(), lg), nil, layersPerEpochBig, testTickSize, goldenATXID, &ValidatorMock{}, lg.WithName("atxHandler"))

	validAtx := newAtx(newChallenge(0, *types.EmptyATXID, goldenATXID, types.LayerID{}), sig, nil, 0, types.Address{})
	err := atxHdlr.ContextuallyValidateAtx(&validAtx.ActivationTxHeader)
	r.NoError(err)

	arbitraryAtxID := types.ATXID(types.HexToHash32("11111"))
	malformedAtx := newAtx(newChallenge(0, arbitraryAtxID, goldenATXID, types.LayerID{}), sig, nil, 0, types.Address{})
	err = atxHdlr.ContextuallyValidateAtx(&malformedAtx.ActivationTxHeader)
	r.ErrorIs(err, sql.ErrNotFound)
}

func TestHandler_HandleAtxNilNipst(t *testing.T) {
	atxHdlr := getATXHandler(t, newCachedDB(t))
	atx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.LayerID{}, 0, 0, coinbase, 0, nil)
	buf, err := codec.Encode(atx)
	require.NoError(t, err)
	require.Error(t, atxHdlr.HandleAtxData(context.TODO(), buf))
}

func TestHandler_KnownATX(t *testing.T) {
	atxHdlr := getATXHandler(t, newCachedDB(t))
	atx := newActivationTx(sig, 0, *types.EmptyATXID, *types.EmptyATXID, types.LayerID{}, 0, 0, coinbase, 0, nil)
	require.NoError(t, atxHdlr.ProcessAtx(context.TODO(), atx))
	require.NoError(t, SignAtx(sig, atx))
	buf, err := codec.Encode(atx)
	require.NoError(t, err)

	require.NoError(t, atxHdlr.HandleAtxData(context.TODO(), buf))
	require.Equal(t, pubsub.ValidationIgnore, atxHdlr.HandleGossipAtx(context.TODO(), "", buf))
}

func BenchmarkGetAtxHeaderWithConcurrentStoreAtx(b *testing.B) {
	lg := logtest.New(b)
	cdb := newCachedDB(b)
	atxHdlr := NewHandler(cdb, nil, 288, testTickSize, types.ATXID{}, &Validator{}, lg)

	var (
		stop uint64
		wg   sync.WaitGroup
	)
	for i := 0; i < runtime.NumCPU()/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pub, _, _ := ed25519.GenerateKey(nil)
			id := types.BytesToNodeID(pub)
			for i := 0; ; i++ {
				atx := types.NewActivationTx(newChallenge(uint64(i), *types.EmptyATXID, goldenATXID, types.NewLayerID(0)), id, types.Address{}, nil, 0, nil)
				if !assert.NoError(b, atxHdlr.StoreAtx(context.TODO(), types.EpochID(1), atx)) {
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
		_, err := atxHdlr.cdb.GetAtxHeader(types.ATXID{1, 1, 1})
		require.ErrorIs(b, err, sql.ErrNotFound)
	}
	atomic.StoreUint64(&stop, 1)
	wg.Wait()
}

// Check that we're not trying to sync an ATX that references the golden ATX or an empty ATX (i.e. not adding it to the sync queue).
func TestHandler_FetchAtxReferences(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	mockFetch := mocks.NewMockFetcher(gomock.NewController(t))
	atxHdlr := NewHandler(newCachedDB(t), mockFetch, layersPerEpoch, testTickSize,
		goldenATXID, &ValidatorMock{}, logtest.New(t).WithName("atxHandler"))
	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)

	atx1 := newAtx(challenge, sig, nipost, 2, coinbase)
	atx1.PositioningATX = types.ATXID{1, 2, 3} // should be fetched
	atx1.PrevATXID = types.ATXID{4, 5, 6}      // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx1.PositioningATX, atx1.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx1))

	atx2 := newAtx(challenge, sig, nipost, 2, coinbase)
	atx2.PositioningATX = goldenATXID     // should *NOT* be fetched
	atx2.PrevATXID = types.ATXID{2, 3, 4} // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx2.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx2))

	atx3 := newAtx(challenge, sig, nipost, 2, coinbase)
	atx3.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx3.PrevATXID = types.ATXID{3, 4, 5}   // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx3.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx3))

	atx4 := newAtx(challenge, sig, nipost, 2, coinbase)
	atx4.PositioningATX = types.ATXID{5, 6, 7} // should be fetched
	atx4.PrevATXID = *types.EmptyATXID         // should *NOT* be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx4.PositioningATX}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx4))

	atx5 := newAtx(challenge, sig, nipost, 2, coinbase)
	atx5.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx5.PrevATXID = *types.EmptyATXID      // should *NOT* be fetched
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx5))

	atx6 := newAtx(challenge, sig, nipost, 2, coinbase)
	atxid := types.ATXID{1, 2, 3}
	atx6.PositioningATX = atxid // should be fetched
	atx6.PrevATXID = atxid      // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxid}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx6))
}

func TestHandler_AtxWeight(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mfetch := mocks.NewMockFetcher(ctrl)
	mvalidator := amocks.NewMocknipostValidator(ctrl)

	const (
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	db := newCachedDB(t)
	handler := NewHandler(db, mfetch, layersPerEpoch, tickSize,
		goldenATXID, mvalidator, logtest.New(t))

	atx1 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			ActivationTxHeader: types.ActivationTxHeader{
				NIPostChallenge: types.NIPostChallenge{
					PositioningATX:     goldenATXID,
					InitialPostIndices: []byte{1},
					PubLayerID:         types.NewLayerID(1).Add(layersPerEpoch),
				},
				NumUnits: units,
			},
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			InitialPost: &types.Post{Indices: []byte{1}},
		},
	}
	require.NoError(t, SignAtx(sig, atx1))
	atx1.CalcAndSetID()

	buf, err := codec.Encode(atx1)
	require.NoError(t, err)

	mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().ValidatePost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaves, nil).Times(1)
	require.NoError(t, handler.HandleAtxData(context.TODO(), buf))

	stored1, err := db.GetAtxHeader(atx1.ID())
	require.NoError(t, err)
	require.Equal(t, uint64(0), stored1.BaseTickHeight())
	require.Equal(t, leaves/tickSize, stored1.TickCount())
	require.Equal(t, leaves/tickSize, stored1.TickHeight())
	require.Equal(t, (leaves/tickSize)*units, stored1.GetWeight())

	atx2 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			ActivationTxHeader: types.ActivationTxHeader{
				NIPostChallenge: types.NIPostChallenge{
					Sequence:       1,
					PositioningATX: atx1.ID(),
					PrevATXID:      atx1.ID(),
					PubLayerID:     types.NewLayerID(1).Add(2 * layersPerEpoch),
				},
				NumUnits: units,
			},
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
		},
	}
	require.NoError(t, SignAtx(sig, atx2))
	atx2.CalcAndSetID()

	buf, err = codec.Encode(atx2)
	require.NoError(t, err)

	mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(1)
	mfetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaves, nil).Times(1)
	require.NoError(t, handler.HandleAtxData(context.TODO(), buf))

	stored2, err := db.GetAtxHeader(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight())
	require.Equal(t, leaves/tickSize, stored2.TickCount())
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.GetWeight()))
}
