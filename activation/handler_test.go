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

func TestHandler_StoreAtx(t *testing.T) {
	// Arrange
	r := require.New(t)
	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, validator, log)

	coinbase1 := types.GenerateAddress([]byte("aaaa"))

	// Act
	epoch1 := types.EpochID(1)
	atx1 := newActivationTx(t, sig, 0, *types.EmptyATXID, goldenATXID, nil, epoch1.FirstLayer(), 0, 100, coinbase1, 0, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.TODO(), atx1))

	epoch2 := types.EpochID(2)
	atx2 := newActivationTx(t, sig, 1, atx1.ID(), atx1.ID(), nil, epoch2.FirstLayer(), 0, 100, coinbase1, 0, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.TODO(), atx2))

	// Assert
	atxid, err := atxs.GetLastIDByNodeID(cdb, sig.NodeID())
	r.NoError(err)
	r.Equal(atx2.ID(), atxid, "atx2.ShortString(): %v", atx2.ShortString())
}

func TestHandler_processBlockATXs(t *testing.T) {
	// Arrange
	r := require.New(t)

	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, log)

	coinbase1 := types.GenerateAddress([]byte("aaaa"))
	coinbase2 := types.GenerateAddress([]byte("bbbb"))
	coinbase3 := types.GenerateAddress([]byte("cccc"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	numTicks := uint64(100)
	numUnits := uint(100)

	npst := newNIPostWithChallenge(&chlng, poetRef)
	posATX := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1000), 0, numTicks, coinbase1, numUnits, npst)
	r.NoError(atxHdlr.StoreAtx(context.TODO(), posATX))

	// Act
	atxList := []*types.VerifiedActivationTx{
		newActivationTx(t, sig, 0, *types.EmptyATXID, posATX.ID(), nil, types.NewLayerID(1012), 0, numTicks, coinbase1, numUnits, &types.NIPost{}),
		newActivationTx(t, sig, 0, *types.EmptyATXID, posATX.ID(), nil, types.NewLayerID(1300), 0, numTicks, coinbase2, numUnits, &types.NIPost{}),
		newActivationTx(t, sig, 0, *types.EmptyATXID, posATX.ID(), nil, types.NewLayerID(1435), 0, numTicks, coinbase3, numUnits, &types.NIPost{}),
	}
	for _, atx := range atxList {
		hash, err := atx.NIPostChallenge.Hash()
		r.NoError(err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)

		r.NoError(atxHdlr.ProcessAtx(context.TODO(), atx))
	}

	// check that further atxList don't affect current epoch count
	atxList2 := []*types.VerifiedActivationTx{
		newActivationTx(t, sig, 1, atxList[0].ID(), atxList[0].ID(), nil, types.NewLayerID(2012), 0, numTicks, coinbase1, numUnits, &types.NIPost{}),
		newActivationTx(t, sig, 1, atxList[1].ID(), atxList[1].ID(), nil, types.NewLayerID(2300), 0, numTicks, coinbase2, numUnits, &types.NIPost{}),
		newActivationTx(t, sig, 1, atxList[2].ID(), atxList[2].ID(), nil, types.NewLayerID(2435), 0, numTicks, coinbase3, numUnits, &types.NIPost{}),
	}
	for _, atx := range atxList2 {
		hash, err := atx.NIPostChallenge.Hash()
		r.NoError(err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)

		r.NoError(atxHdlr.ProcessAtx(context.TODO(), atx))
	}

	// Assert
	// 1 posATX + 3 atx from `atxList`; weight = 4 * numTicks * numUnit
	epochWeight, _, err := cdb.GetEpochWeight(2)
	r.NoError(err)
	r.Equal(4*numTicks*uint64(numUnits), epochWeight)

	// 3 atx from `atxList2`; weight = 3 * numTicks * numUnit
	epochWeight, _, err = cdb.GetEpochWeight(3)
	r.NoError(err)
	r.Equal(3*numTicks*uint64(numUnits), epochWeight)
}

func TestHandler_SyntacticallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	validator.EXPECT().ValidatePost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	validator.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).AnyTimes()
	atxHdlr := NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, log)

	otherSig := NewMockSigner()
	coinbase := types.GenerateAddress([]byte("aaaa"))

	sig1 := NewMockSigner()
	sig2 := NewMockSigner()
	sig3 := NewMockSigner()
	atxList := []*types.VerifiedActivationTx{
		newActivationTx(t, sig1, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(t, sig2, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(t, sig3, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
	}
	for _, atx := range atxList {
		require.NoError(t, atxHdlr.ProcessAtx(context.TODO(), atx))
	}

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := newNIPostWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	posAtx := newActivationTx(t, otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), prevAtx))
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), posAtx))

	// Act & Assert

	t.Run("valid atx", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		hash, err := atx.NIPostChallenge.Hash()
		require.NoError(t, err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)
		require.NoError(t, SignAtx(sig, atx))

		vAtx, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.ContextuallyValidateAtx(vAtx))
	})

	t.Run("wrong sequence", func(t *testing.T) {
		challenge := newChallenge(0, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "sequence number is not one more than prev sequence number")
	})

	t.Run("wrong positioning atx", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), atxList[0].ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "expected distance of one epoch (1000 layers) from positioning atx but found 1011")
	})

	t.Run("wrong commitment atx", func(t *testing.T) {
		cATX := atxList[0].ID()
		challenge := newChallenge(0, *types.EmptyATXID, prevAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "expected distance of one epoch (1000 layers) from commitment atx but found 1011")
	})

	t.Run("empty positioning atx", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), *types.EmptyATXID, types.NewLayerID(2000), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 3, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "empty positioning atx")
	})

	t.Run("golden atx as posAtx in epoch 2", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), goldenATXID, types.NewLayerID(2000), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 3, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "golden atx used for positioning atx in epoch 2, but is only valid in epoch 1")
	})

	t.Run("golden atx as commitmentAtx in epoch 2", func(t *testing.T) {
		challenge := newChallenge(0, *types.EmptyATXID, prevAtxID, types.NewLayerID(2000), &goldenATXID)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 3, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "golden atx used for commitment atx in epoch 2, but is only valid in epoch 1")
	})

	t.Run("invalid prevAtx", func(t *testing.T) {
		challenge := newChallenge(1, atxList[0].ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.ErrorContains(t, err, "previous atx belongs to different miner")
		require.ErrorContains(t, err, fmt.Sprintf("atx.ID: %v", atx.ShortString()))
		require.ErrorContains(t, err, fmt.Sprintf("atx.NodeID: %v", atx.NodeID()))
		require.ErrorContains(t, err, fmt.Sprintf("prevAtx.ID: %v", atxList[0].ID()))
		require.ErrorContains(t, err, fmt.Sprintf("prevAtx.NodeID: %v", atxList[0].NodeID()))
	})

	t.Run("wrong layer for positioning atx", func(t *testing.T) {
		posAtx2 := newActivationTx(t, otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1020), 0, 100, coinbase, 100, npst)
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), posAtx2))
		challenge := newChallenge(1, prevAtx.ID(), posAtx2.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, npst, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "atx layer (1012) must be after positioning atx layer (1020)")
	})

	t.Run("wrong layer for commitment atx", func(t *testing.T) {
		commitmentAtx := newActivationTx(t, otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1020), 0, 100, coinbase, 100, npst)
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), commitmentAtx))

		cATX := commitmentAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, challenge, sig, npst, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "atx layer (1012) must be after commitment atx layer (1020)")
	})

	t.Run("prevAtx not declared but sequence number not zero", func(t *testing.T) {
		challenge := newChallenge(1, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "no prevATX declared, but sequence number not zero")
	})

	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "no prevATX declared, but initial Post is not included")
	})

	t.Run("prevAtx not declared but initial Post indices not included", func(t *testing.T) {
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "no prevATX declared, but initial Post indices is not included in challenge")
	})

	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		validator := amocks.NewMocknipostValidator(gomock.NewController(t))
		validator.EXPECT().ValidatePost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed validation")).Times(1)
		atxHdlr := NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, log)

		cATX := prevAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, challenge, sig, npst, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "invalid initial Post: failed validation")
	})

	t.Run("challenge and initial Post indices mismatch", func(t *testing.T) {
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = append([]byte{}, initialPost.Indices...)
		atx.InitialPostIndices[0]++
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "initial Post indices included in challenge does not equal to the initial Post indices included in the atx")
	})

	t.Run("prevAtx not declared but commitmentAtx is missing", func(t *testing.T) {
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "no prevATX declared, but commitmentATX is missing")
	})

	t.Run("commitmentATX declared but not found", func(t *testing.T) {
		cATX := types.RandomATXID()
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "commitment atx not found")
	})

	t.Run("prevAtx declared but not found", func(t *testing.T) {
		challenge := newChallenge(1, types.RandomATXID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.ErrorContains(t, err, "prevATX not found")
	})

	t.Run("posAtx declared but not found", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), types.RandomATXID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "positioning atx not found")
	})

	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "prevATX declared, but initial Post is included")
	})

	t.Run("prevAtx declared but initial Post indices is included", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		atx.InitialPostIndices = initialPost.Indices
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "prevATX declared, but initial Post indices is included in challenge")
	})

	t.Run("prevAtx declared but commitmentAtx is included", func(t *testing.T) {
		cATX := prevAtx.ID()
		challenge := newChallenge(1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "prevATX declared, but commitmentATX is included")
	})

	t.Run("prevAtx has publication layer in the same epoch as the atx", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(100), nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		_, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
		require.EqualError(t, err, "prevAtx epoch (0, layer 100) isn't older than current atx epoch (0, layer 100)")
	})
}

func TestHandler_ContextuallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	log := logtest.New(t).WithName("sigValidation")
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, log.WithName("atxHandler"))

	sig1 := NewMockSigner()
	otherSig := NewMockSigner()

	coinbase := types.GenerateAddress([]byte("aaaa"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := newNIPostWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), prevAtx))

	// Act & Assert

	t.Run("valid atx", func(t *testing.T) {
		challenge := newChallenge(1, *types.EmptyATXID, goldenATXID, types.LayerID{}, nil)
		atx, err := newAtx(t, challenge, sig1, nil, 0, types.Address{}).Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.ContextuallyValidateAtx(atx))
	})

	t.Run("atx already exists", func(t *testing.T) {
		challenge := newChallenge(1, *types.EmptyATXID, goldenATXID, types.LayerID{}, nil)
		atx := newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx))

		challenge = newChallenge(1, prevAtx.ID(), goldenATXID, types.NewLayerID(12), nil)
		atx = newAtx(t, challenge, sig, &types.NIPost{}, 100, coinbase)
		vAtx, err = atx.Verify(0, 1)
		require.NoError(t, err)
		err = atxHdlr.ContextuallyValidateAtx(vAtx)
		require.EqualError(t, err, "last atx is not the one referenced")
	})

	t.Run("missing prevAtx", func(t *testing.T) {
		arbitraryAtxID := types.ATXID(types.HexToHash32("11111"))
		atx, err := newAtx(t, newChallenge(1, arbitraryAtxID, prevAtx.ID(), types.LayerID{}, nil), sig1, nil, 0, types.Address{}).Verify(0, 1)
		require.NoError(t, err)
		err = atxHdlr.ContextuallyValidateAtx(atx)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx by same node", func(t *testing.T) {
		vAtx := newActivationTx(t, sig1, 1, prevAtx.ID(), prevAtx.ID(), nil, types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx))

		vAtx2 := newActivationTx(t, sig1, 2, vAtx.ID(), vAtx.ID(), nil, types.NewLayerID(1012+layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx2))

		vAtx3 := newActivationTx(t, sig1, 3, vAtx.ID(), vAtx.ID(), nil, types.NewLayerID(1012+layersPerEpoch*3), 0, 100, coinbase, 100, &types.NIPost{})
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx3), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(cdb, sig1.NodeID())
		require.NoError(t, err)
		require.Equal(t, vAtx2.ID(), id)
		_, err = cdb.GetAtxHeader(vAtx3.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx from different node", func(t *testing.T) {
		vAtx := newActivationTx(t, sig1, 1, prevAtx.ID(), prevAtx.ID(), nil, types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx))

		prevAtx := newActivationTx(t, otherSig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), prevAtx))

		vAtx1 := newActivationTx(t, otherSig, 1, prevAtx.ID(), prevAtx.ID(), nil, types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx1))

		vAtx2 := newActivationTx(t, otherSig, 2, vAtx.ID(), vAtx.ID(), nil, types.NewLayerID(1012+layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx2), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(cdb, otherSig.NodeID())
		require.NoError(t, err)
		require.Equal(t, vAtx1.ID(), id)
		_, err = cdb.GetAtxHeader(vAtx2.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
}

func TestHandler_ProcessAtx(t *testing.T) {
	// Arrange
	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, validator, log)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(100), 0, 100, coinbase, 100, &types.NIPost{})
	require.NoError(t, atxHdlr.ProcessAtx(context.TODO(), atx1))

	// processing an already stored ATX returns no error
	atx2 := newActivationTx(t, sig, 1, atx1.ID(), atx1.ID(), nil, types.NewLayerID(100), 0, 100, coinbase, 100, &types.NIPost{})
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), atx2))
	require.NoError(t, atxHdlr.ProcessAtx(context.TODO(), atx2))
}

func BenchmarkActivationDb_SyntacticallyValidateAtx(b *testing.B) {
	r := require.New(b)
	log := logtest.New(b)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(b))
	validator.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).AnyTimes()
	atxHdlr := NewHandler(cdb, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, log)

	const (
		activesetSize         = 300
		numberOfLayers uint32 = 100
	)

	coinbase := types.GenerateAddress([]byte("c012ba5e"))
	var atxList []*types.VerifiedActivationTx
	for i := 0; i < activesetSize; i++ {
		atxList = append(atxList, newActivationTx(b, sig, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}))
	}

	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxList {
		hash, err := atx.NIPostChallenge.Hash()
		r.NoError(err)
		atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	}

	challenge := newChallenge(0, *types.EmptyATXID, goldenATXID, types.LayerID{}.Add(numberOfLayers+1), &goldenATXID)
	hash, err := challenge.Hash()
	r.NoError(err)
	npst := newNIPostWithChallenge(hash, poetRef)
	prevAtx := newAtx(b, challenge, sig, npst, 2, coinbase)

	challenge = newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.LayerID{}.Add(numberOfLayers+1+layersPerEpochBig), nil)
	atx := newAtx(b, challenge, sig, &types.NIPost{}, 100, coinbase)
	hash, err = atx.NIPostChallenge.Hash()
	r.NoError(err)
	atx.NIPost = newNIPostWithChallenge(hash, poetRef)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxHdlr.StoreAtx(context.TODO(), vPrevAtx))

	start := time.Now()
	_, err = atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	b.Logf("\nSyntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	vAtx, err := atxHdlr.SyntacticallyValidateAtx(context.TODO(), atx)
	b.Logf("\nSecond syntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxHdlr.ContextuallyValidateAtx(vAtx)
	b.Logf("\nContextual validation took %v\n\n", time.Since(start))
	r.NoError(err)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)
	log := logtest.New(b)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(b))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, validator, log.WithName("atxHandler"))

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
			challenge := newChallenge(1, prevAtxs[miner], posAtx, layer, nil)
			h, err := challenge.Hash()
			r.NoError(err)
			npst := newNIPostWithChallenge(h, poetBytes)
			atx = newAtx(b, challenge, sig, npst, 2, coinbase)
			prevAtxs[miner] = atx.ID()
			vAtx, err := atx.Verify(0, 1)
			r.NoError(err)
			require.NoError(b, atxHdlr.StoreAtx(context.TODO(), vAtx))
		}
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
}

func TestHandler_GetPosAtx(t *testing.T) {
	// Arrange
	r := require.New(t)

	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, validator, log)

	// Act & Assert

	// ATX stored should become top ATX
	atx := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}.Add(1), 0, 100, coinbase, 100, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.TODO(), atx))

	id, err := atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx.ID(), id)

	// higher-layer ATX stored should become new top ATX
	atx = newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}.Add(layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.TODO(), atx))

	id, err = atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx.ID(), id)

	// lower-layer ATX stored should NOT become new top ATX
	atx = newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}.Add(1), 0, 100, coinbase, 100, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.TODO(), atx))

	id, err = atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.NotEqual(atx.ID(), id)
}

func TestHandler_AwaitAtx(t *testing.T) {
	// Arrange
	r := require.New(t)

	log := logtest.New(t).WithName("sigValidation")
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, validator, log.WithName("atxHandler"))

	// Act & Assert

	atx := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{})
	ch := atxHdlr.AwaitAtx(atx.ID())
	r.Len(atxHdlr.atxChannels, 1) // channel was created

	select {
	case <-ch:
		r.Fail("notified before ATX was stored")
	default:
	}

	err := atxHdlr.StoreAtx(context.TODO(), atx)
	r.NoError(err)
	r.Len(atxHdlr.atxChannels, 0) // after notifying subscribers, channel is cleared

	select {
	case <-ch:
	default:
		r.Fail("not notified after ATX was stored")
	}

	ch = atxHdlr.AwaitAtx(atx.ID())
	r.Len(atxHdlr.atxChannels, 0) // awaiting an already stored ATX should not create a channel, but return a closed one

	select {
	case <-ch:
	default:
		r.Fail("open channel returned for already stored ATX")
	}

	otherID := types.ATXID(types.HexToHash32("abcd"))
	atxHdlr.AwaitAtx(otherID)
	r.Len(atxHdlr.atxChannels, 1) // after first subscription - channel is created
	atxHdlr.AwaitAtx(otherID)
	r.Len(atxHdlr.atxChannels, 1) // second subscription to same id - no additional channel
	atxHdlr.UnsubscribeAtx(otherID)
	r.Len(atxHdlr.atxChannels, 1) // first unsubscribe doesn't clear the channel
	atxHdlr.UnsubscribeAtx(otherID)
	r.Len(atxHdlr.atxChannels, 0) // last unsubscribe clears the channel
}

func TestHandler_HandleAtxData(t *testing.T) {
	// Arrange
	log := logtest.New(t).WithName("sigValidation")
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, validator, log.WithName("atxHandler"))

	// Act & Assert

	t.Run("missing nipost", func(t *testing.T) {
		atx := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}, 0, 0, coinbase, 0, nil)
		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.EqualError(t, atxHdlr.HandleAtxData(context.TODO(), buf), fmt.Sprintf("nil nipst in gossip for atx %v", atx.ID()))
	})

	t.Run("known atx is ignored by handleAtxData", func(t *testing.T) {
		atx := newActivationTx(t, sig, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}, 0, 0, coinbase, 0, nil)
		require.NoError(t, atxHdlr.ProcessAtx(context.TODO(), atx))
		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.NoError(t, atxHdlr.HandleAtxData(context.TODO(), buf))
		require.Equal(t, pubsub.ValidationIgnore, atxHdlr.HandleGossipAtx(context.TODO(), "", buf))
	})
}

func BenchmarkGetAtxHeaderWithConcurrentStoreAtx(b *testing.B) {
	log := logtest.New(b)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	validator := amocks.NewMocknipostValidator(gomock.NewController(b))
	atxHdlr := NewHandler(cdb, nil, 288, testTickSize, types.ATXID{}, validator, log)

	var (
		stop uint64
		wg   sync.WaitGroup
	)
	for i := 0; i < runtime.NumCPU()/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				challenge := newChallenge(uint64(i), *types.EmptyATXID, goldenATXID, types.NewLayerID(0), nil)
				atx := newAtx(b, challenge, sig, nil, 0, types.Address{})
				vAtx, err := atx.Verify(0, 1)
				if !assert.NoError(b, err) {
					return
				}
				if !assert.NoError(b, atxHdlr.StoreAtx(context.TODO(), vAtx)) {
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
	ctrl := gomock.NewController(t)
	mockFetch := mocks.NewMockFetcher(ctrl)
	validator := amocks.NewMocknipostValidator(gomock.NewController(t))

	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)

	atxHdlr := NewHandler(cdb, mockFetch, layersPerEpoch, testTickSize, goldenATXID, validator, log.WithName("atxHandler"))
	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)

	atx1 := newAtx(t, challenge, sig, nipost, 2, coinbase)
	atx1.PositioningATX = types.ATXID{1, 2, 3} // should be fetched
	atx1.PrevATXID = types.ATXID{4, 5, 6}      // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx1.PositioningATX, atx1.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx1))

	atx2 := newAtx(t, challenge, sig, nipost, 2, coinbase)
	atx2.PositioningATX = goldenATXID     // should *NOT* be fetched
	atx2.PrevATXID = types.ATXID{2, 3, 4} // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx2.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx2))

	atx3 := newAtx(t, challenge, sig, nipost, 2, coinbase)
	atx3.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx3.PrevATXID = types.ATXID{3, 4, 5}   // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx3.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx3))

	atx4 := newAtx(t, challenge, sig, nipost, 2, coinbase)
	atx4.PositioningATX = types.ATXID{5, 6, 7} // should be fetched
	atx4.PrevATXID = *types.EmptyATXID         // should *NOT* be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx4.PositioningATX}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx4))

	atx5 := newAtx(t, challenge, sig, nipost, 2, coinbase)
	atx5.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx5.PrevATXID = *types.EmptyATXID      // should *NOT* be fetched
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx5))

	atx6 := newAtx(t, challenge, sig, nipost, 2, coinbase)
	atxid := types.ATXID{1, 2, 3}
	atx6.PositioningATX = atxid // should be fetched
	atx6.PrevATXID = atxid      // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxid}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.TODO(), atx6))
}

func TestHandler_AtxWeight(t *testing.T) {
	ctrl := gomock.NewController(t)
	mfetch := mocks.NewMockFetcher(ctrl)
	mvalidator := amocks.NewMocknipostValidator(ctrl)

	const (
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	log := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), log)
	handler := NewHandler(cdb, mfetch, layersPerEpoch, tickSize, goldenATXID, mvalidator, log)

	atx1 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PositioningATX:     goldenATXID,
				InitialPostIndices: []byte{1},
				PubLayerID:         types.NewLayerID(1).Add(layersPerEpoch),

				CommitmentATX: &goldenATXID,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			InitialPost: &types.Post{Indices: []byte{1}},
		},
	}
	require.NoError(t, SignAtx(sig, atx1))
	require.NoError(t, atx1.CalcAndSetID())

	buf, err := codec.Encode(atx1)
	require.NoError(t, err)

	mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().ValidatePost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaves, nil).Times(1)
	require.NoError(t, handler.HandleAtxData(context.TODO(), buf))

	stored1, err := cdb.GetAtxHeader(atx1.ID())
	require.NoError(t, err)
	require.Equal(t, uint64(0), stored1.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored1.TickCount)
	require.Equal(t, leaves/tickSize, stored1.TickHeight())
	require.Equal(t, (leaves/tickSize)*units, stored1.GetWeight())

	atx2 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:       1,
				PositioningATX: atx1.ID(),
				PrevATXID:      atx1.ID(),
				PubLayerID:     types.NewLayerID(1).Add(2 * layersPerEpoch),
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
		},
	}
	require.NoError(t, SignAtx(sig, atx2))
	require.NoError(t, atx2.CalcAndSetID())

	buf, err = codec.Encode(atx2)
	require.NoError(t, err)

	mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(1)
	mfetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaves, nil).Times(1)
	require.NoError(t, handler.HandleAtxData(context.TODO(), buf))

	stored2, err := cdb.GetAtxHeader(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored2.TickCount)
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.GetWeight()))
}
