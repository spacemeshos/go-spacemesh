package activation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/vrfnonce"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const layersPerEpochBig = 1000

func newNIPostWithChallenge(challenge types.Hash32, poetRef []byte) *types.NIPost {
	return &types.NIPost{
		Challenge: &challenge,
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
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpoch, testTickSize, goldenATXID, validator, receiver, lg)

	coinbase1 := types.GenerateAddress([]byte("aaaa"))

	// Act
	epoch1 := types.EpochID(1)
	atx1 := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, goldenATXID, nil, epoch1.FirstLayer(), 0, 100, coinbase1, 0, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.Background(), atx1))

	epoch2 := types.EpochID(2)
	atx2 := newActivationTx(t, sig, &nid, 1, atx1.ID(), atx1.ID(), nil, epoch2.FirstLayer(), 0, 100, coinbase1, 0, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.Background(), atx2))

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

	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nid := sig.NodeID()
	sig1, err := signing.NewEdSigner()
	r.NoError(err)
	nid1 := sig1.NodeID()
	sig2, err := signing.NewEdSigner()
	r.NoError(err)
	nid2 := sig2.NodeID()
	sig3, err := signing.NewEdSigner()
	r.NoError(err)
	nid3 := sig3.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

	coinbase1 := types.GenerateAddress([]byte("aaaa"))
	coinbase2 := types.GenerateAddress([]byte("bbbb"))
	coinbase3 := types.GenerateAddress([]byte("cccc"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	numTicks := uint64(100)
	numUnits := uint32(100)

	npst := newNIPostWithChallenge(chlng, poetRef)
	posATX := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1000), 0, numTicks, coinbase1, numUnits, npst)
	r.NoError(atxHdlr.StoreAtx(context.Background(), posATX))

	// Act
	atxList := []*types.VerifiedActivationTx{
		newActivationTx(t, sig1, &nid1, 0, *types.EmptyATXID, posATX.ID(), nil, types.NewLayerID(1012), 0, numTicks, coinbase1, numUnits, &types.NIPost{}),
		newActivationTx(t, sig2, &nid2, 0, *types.EmptyATXID, posATX.ID(), nil, types.NewLayerID(1300), 0, numTicks, coinbase2, numUnits, &types.NIPost{}),
		newActivationTx(t, sig3, &nid3, 0, *types.EmptyATXID, posATX.ID(), nil, types.NewLayerID(1435), 0, numTicks, coinbase3, numUnits, &types.NIPost{}),
	}
	for _, atx := range atxList {
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)

		r.NoError(atxHdlr.ProcessAtx(context.Background(), atx))
	}

	// check that further atxList don't affect current epoch count
	atxList2 := []*types.VerifiedActivationTx{
		newActivationTx(t, sig1, &nid1, 1, atxList[0].ID(), atxList[0].ID(), nil, types.NewLayerID(2012), 0, numTicks, coinbase1, numUnits, &types.NIPost{}),
		newActivationTx(t, sig2, &nid2, 1, atxList[1].ID(), atxList[1].ID(), nil, types.NewLayerID(2300), 0, numTicks, coinbase2, numUnits, &types.NIPost{}),
		newActivationTx(t, sig3, &nid3, 1, atxList[2].ID(), atxList[2].ID(), nil, types.NewLayerID(2435), 0, numTicks, coinbase3, numUnits, &types.NIPost{}),
	}
	for _, atx := range atxList2 {
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)

		r.NoError(atxHdlr.ProcessAtx(context.Background(), atx))
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

	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid := sig.NodeID()
	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)
	otherNid := otherSig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, nil, lg)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid1 := sig1.NodeID()
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid2 := sig2.NodeID()
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid3 := sig3.NodeID()
	atxList := []*types.VerifiedActivationTx{
		newActivationTx(t, sig1, &nid1, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(t, sig2, &nid2, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
		newActivationTx(t, sig3, &nid3, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}),
	}
	for _, atx := range atxList {
		require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx))
	}

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := newNIPostWithChallenge(chlng, poetRef)
	prevAtx := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	posAtx := newActivationTx(t, otherSig, &otherNid, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	require.NoError(t, atxHdlr.StoreAtx(context.Background(), prevAtx))
	require.NoError(t, atxHdlr.StoreAtx(context.Background(), posAtx))

	// Act & Assert

	t.Run("valid atx", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).Times(1)
		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("valid atx with new VRF nonce", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), nil)
		nonce := types.VRFPostIndex(999)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		atx.VRFNonce = &nonce
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).Times(1)
		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("valid atx with decreasing num units", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 90, coinbase) // numunits decreased from 100 to 90 between atx and prevAtx
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).Times(1)
		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("invalid atx with increasing num units", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 110, coinbase) // numunits increased from 100 to 110 between atx and prevAtx
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.EqualError(t, err, "num units 110 is greater than previous atx num units 100")
	})

	initialPost := &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}

	t.Run("valid initial atx", func(t *testing.T) {
		ctxID := prevAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, prevAtx.ID(), types.NewLayerID(1012), &ctxID)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("failing nipost challenge validation", func(t *testing.T) {
		challenge := newChallenge(0, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)

		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("nipost error")).Times(1)

		_, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.EqualError(t, err, "nipost error")
	})

	t.Run("failing positioning atx validation", func(t *testing.T) {
		challenge := newChallenge(0, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)

		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("bad positioning atx")).Times(1)

		_, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.EqualError(t, err, "bad positioning atx")
	})

	t.Run("bad initial nipost challenge", func(t *testing.T) {
		commitmentAtx := newActivationTx(t, otherSig, &otherNid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1020), 0, 100, coinbase, 100, npst)
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), commitmentAtx))

		cATX := commitmentAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, sig, &nid, challenge, npst, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices

		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("bad initial nipost")).Times(1)

		_, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.EqualError(t, err, "bad initial nipost")
	})

	t.Run("missing VRF nonce in initial atx", func(t *testing.T) {
		ctxID := prevAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, prevAtx.ID(), types.NewLayerID(1012), &ctxID)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.ErrorContains(t, err, "VRFNonce is missing")
	})

	t.Run("invalid VRF nonce in initial atx", func(t *testing.T) {
		ctxID := prevAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, prevAtx.ID(), types.NewLayerID(1012), &ctxID)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(errors.New("invalid VRF nonce"))
		validator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF nonce")
	})

	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.InitialPostIndices = []byte{}
		atx.CommitmentATX = &goldenATXID

		_, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.EqualError(t, err, "no prevATX declared, but initial Post is not included")
	})

	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		cATX := prevAtx.ID()
		challenge := newChallenge(0, *types.EmptyATXID, posAtx.ID(), types.NewLayerID(1012), &cATX)
		atx := newAtx(t, sig, &nid, challenge, npst, 100, coinbase)
		atx.InitialPost = initialPost
		atx.InitialPostIndices = initialPost.Indices

		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("failed post validation")).Times(1)

		_, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.ErrorContains(t, err, "failed post validation")
	})

	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		challenge := newChallenge(1, prevAtx.ID(), posAtx.ID(), types.NewLayerID(1012), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		atx.InitialPost = initialPost

		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		_, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
		require.EqualError(t, err, "prevATX declared, but initial Post is included")
	})
}

func TestHandler_ContextuallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	lg := logtest.New(t).WithName("sigValidation")
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

	coinbase := types.GenerateAddress([]byte("aaaa"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := newNIPostWithChallenge(chlng, poetRef)
	prevAtx := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
	require.NoError(t, atxHdlr.StoreAtx(context.Background(), prevAtx))

	// Act & Assert

	t.Run("valid atx", func(t *testing.T) {
		sig1, err := signing.NewEdSigner()
		require.NoError(t, err)
		nid1 := sig1.NodeID()
		challenge := newChallenge(1, *types.EmptyATXID, goldenATXID, types.LayerID{}, nil)
		atx, err := newAtx(t, sig1, &nid1, challenge, nil, 0, types.Address{}).Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.ContextuallyValidateAtx(atx))
	})

	t.Run("atx already exists", func(t *testing.T) {
		challenge := newChallenge(1, *types.EmptyATXID, goldenATXID, types.NewLayerID(layersPerEpochBig), nil)
		atx := newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), vAtx))

		challenge = newChallenge(1, prevAtx.ID(), goldenATXID, types.NewLayerID(12), nil)
		atx = newAtx(t, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
		vAtx, err = atx.Verify(0, 1)
		require.NoError(t, err)
		err = atxHdlr.ContextuallyValidateAtx(vAtx)
		require.EqualError(t, err, "last atx is not the one referenced")
	})

	t.Run("missing prevAtx", func(t *testing.T) {
		sig1, err := signing.NewEdSigner()
		require.NoError(t, err)
		nid1 := sig1.NodeID()
		arbitraryAtxID := types.ATXID(types.HexToHash32("11111"))
		challenge := newChallenge(1, arbitraryAtxID, prevAtx.ID(), types.LayerID{}, nil)
		atx, err := newAtx(t, sig1, &nid1, challenge, nil, 0, types.Address{}).Verify(0, 1)
		require.NoError(t, err)
		err = atxHdlr.ContextuallyValidateAtx(atx)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx by same node", func(t *testing.T) {
		sig1, err := signing.NewEdSigner()
		require.NoError(t, err)
		nid1 := sig1.NodeID()
		vAtx := newActivationTx(t, sig1, &nid1, 1, prevAtx.ID(), prevAtx.ID(), nil, types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), vAtx))

		vAtx2 := newActivationTx(t, sig1, &nid1, 2, vAtx.ID(), vAtx.ID(), nil, types.NewLayerID(1012+layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), vAtx2))

		vAtx3 := newActivationTx(t, sig1, &nid1, 3, vAtx.ID(), vAtx.ID(), nil, types.NewLayerID(1012+layersPerEpochBig*3), 0, 100, coinbase, 100, &types.NIPost{})
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx3), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(cdb, sig1.NodeID())
		require.NoError(t, err)
		require.Equal(t, vAtx2.ID(), id)
		_, err = cdb.GetAtxHeader(vAtx3.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx from different node", func(t *testing.T) {
		sig1, err := signing.NewEdSigner()
		require.NoError(t, err)
		nid1 := sig1.NodeID()
		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)
		otherNid := otherSig.NodeID()
		vAtx := newActivationTx(t, sig1, &nid1, 1, prevAtx.ID(), prevAtx.ID(), nil, types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), vAtx))

		prevAtx := newActivationTx(t, otherSig, &otherNid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(100), 0, 100, coinbase, 100, npst)
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), prevAtx))

		vAtx1 := newActivationTx(t, otherSig, &otherNid, 1, prevAtx.ID(), prevAtx.ID(), nil, types.NewLayerID(1012), 0, 100, coinbase, 100, &types.NIPost{})
		require.NoError(t, atxHdlr.StoreAtx(context.Background(), vAtx1))

		vAtx2 := newActivationTx(t, otherSig, &otherNid, 2, vAtx.ID(), vAtx.ID(), nil, types.NewLayerID(1012+layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
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
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)

	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)

	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpoch, testTickSize, goldenATXID, validator, receiver, lg)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx1))

	// processing an already stored ATX returns no error
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx1))
	proof, err := identities.GetMalfeasanceProof(cdb, nid)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, proof)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(t, sig, &nid, 1, atx1.ID(), atx1.ID(), nil, types.NewLayerID(layersPerEpoch+1), 0, 100, coinbase, 100, &types.NIPost{})
	mclock.EXPECT().GetCurrentLayer().Return(atx2.PubLayerID)
	var got types.MalfeasanceGossip
	mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			require.NoError(t, codec.Decode(data, &got))
			return nil
		})
	require.ErrorIs(t, atxHdlr.ProcessAtx(context.Background(), atx2), errMaliciousATX)
	proof, err = identities.GetMalfeasanceProof(cdb, nid)
	require.NoError(t, err)
	require.Equal(t, got.MalfeasanceProof, *proof)
}

func TestHandler_ProcessAtxStoresNewVRFNonce(t *testing.T) {
	// Arrange
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)

	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)

	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpoch, testTickSize, goldenATXID, validator, receiver, lg)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	nonce1 := types.VRFPostIndex(123)
	atx1.VRFNonce = &nonce1
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx1))

	got, err := vrfnonce.Get(cdb, nid, atx1.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(t, sig, &nid, 1, atx1.ID(), atx1.ID(), nil, types.NewLayerID(2*layersPerEpoch), 0, 100, coinbase, 100, &types.NIPost{})
	nonce2 := types.VRFPostIndex(456)
	atx2.VRFNonce = &nonce2
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx2))

	got, err = vrfnonce.Get(cdb, nid, atx2.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce2, got)
}

func BenchmarkActivationDb_SyntacticallyValidateAtx(b *testing.B) {
	r := require.New(b)
	lg := logtest.New(b)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(b)
	validator := NewMocknipostValidator(ctrl)
	validator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(1), nil).AnyTimes()
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

	const (
		activesetSize         = 300
		numberOfLayers uint32 = 100
	)

	coinbase := types.GenerateAddress([]byte("c012ba5e"))
	var atxList []*types.VerifiedActivationTx
	for i := 0; i < activesetSize; i++ {
		atxList = append(atxList, newActivationTx(b, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, &goldenATXID, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{}))
	}

	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxList {
		atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
	}

	challenge := newChallenge(0, *types.EmptyATXID, goldenATXID, types.LayerID{}.Add(numberOfLayers+1), &goldenATXID)
	npst := newNIPostWithChallenge(challenge.Hash(), poetRef)
	prevAtx := newAtx(b, sig, &nid, challenge, npst, 2, coinbase)

	challenge = newChallenge(1, prevAtx.ID(), prevAtx.ID(), types.LayerID{}.Add(numberOfLayers+1+layersPerEpochBig), nil)
	atx := newAtx(b, sig, &nid, challenge, &types.NIPost{}, 100, coinbase)
	atx.NIPost = newNIPostWithChallenge(atx.NIPostChallenge.Hash(), poetRef)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxHdlr.StoreAtx(context.Background(), vPrevAtx))

	start := time.Now()
	_, err = atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
	b.Logf("\nSyntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	vAtx, err := atxHdlr.SyntacticallyValidateAtx(context.Background(), atx)
	b.Logf("\nSecond syntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxHdlr.ContextuallyValidateAtx(vAtx)
	b.Logf("\nContextual validation took %v\n\n", time.Since(start))
	r.NoError(err)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)
	lg := logtest.New(b)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(b)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

	const (
		numOfMiners = 300
		batchSize   = 15
		numOfEpochs = 10 * batchSize
	)
	prevAtxs := make([]types.ATXID, numOfMiners)
	pPrevAtxs := make([]types.ATXID, numOfMiners)
	posAtx := types.ATXID{2, 3, 4}
	var atx *types.ActivationTx
	layer := types.NewLayerID(22)
	poetBytes := []byte("66666")
	coinbase := types.Address{2, 4, 5}
	start := time.Now()
	eStart := time.Now()
	for epoch := postGenesisEpoch; epoch < postGenesisEpoch+numOfEpochs; epoch++ {
		for miner := 0; miner < numOfMiners; miner++ {
			challenge := newChallenge(1, prevAtxs[miner], posAtx, layer, nil)
			npst := newNIPostWithChallenge(challenge.Hash(), poetBytes)
			atx = newAtx(b, sig, &nid, challenge, npst, 2, coinbase)
			prevAtxs[miner] = atx.ID()
			vAtx, err := atx.Verify(0, 1)
			r.NoError(err)
			require.NoError(b, atxHdlr.StoreAtx(context.Background(), vAtx))
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

	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nid := sig.NodeID()
	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)
	otherNid := otherSig.NodeID()
	coinbase := types.Address{2, 4, 5}
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

	// Act & Assert

	// ATX stored should become top ATX
	atx1 := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.Background(), atx1))

	id, err := atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx1.ID(), id)

	// higher-layer ATX stored should become new top ATX
	atx2 := newActivationTx(t, otherSig, &otherNid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(2*layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.Background(), atx2))

	id, err = atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.Equal(atx2.ID(), id)

	// lower-layer ATX stored should NOT become new top ATX
	atx3 := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(layersPerEpochBig), 0, 100, coinbase, 100, &types.NIPost{})
	r.NoError(atxHdlr.StoreAtx(context.Background(), atx3))

	id, err = atxHdlr.GetPosAtxID()
	r.NoError(err)
	r.NotEqual(atx3.ID(), id)
	r.Equal(atx2.ID(), id)
}

func TestHandler_AwaitAtx(t *testing.T) {
	// Arrange
	r := require.New(t)

	lg := logtest.New(t).WithName("sigValidation")
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	r.NoError(err)
	nid := sig.NodeID()
	coinbase := types.Address{2, 4, 5}
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)
	// Act & Assert

	atx := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.NewLayerID(1), 0, 100, coinbase, 100, &types.NIPost{})
	ch := atxHdlr.AwaitAtx(atx.ID())
	r.Len(atxHdlr.atxChannels, 1) // channel was created

	select {
	case <-ch:
		r.Fail("notified before ATX was stored")
	default:
	}

	r.NoError(atxHdlr.StoreAtx(context.Background(), atx))
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
	lg := logtest.New(t).WithName("sigValidation")
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid := sig.NodeID()
	coinbase := types.Address{2, 4, 5}
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

	// Act & Assert

	t.Run("missing nipost", func(t *testing.T) {
		atx := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}, 0, 0, coinbase, 0, nil)
		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.EqualError(t, atxHdlr.HandleAtxData(context.Background(), buf), fmt.Sprintf("nil nipst in gossip for atx %v", atx.ID()))
	})

	t.Run("known atx is ignored by handleAtxData", func(t *testing.T) {
		atx := newActivationTx(t, sig, &nid, 0, *types.EmptyATXID, *types.EmptyATXID, nil, types.LayerID{}, 0, 0, coinbase, 0, nil)
		require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx))
		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.NoError(t, atxHdlr.HandleAtxData(context.Background(), buf))
		require.Equal(t, pubsub.ValidationIgnore, atxHdlr.HandleGossipAtx(context.Background(), "", buf))
	})
}

func BenchmarkGetAtxHeaderWithConcurrentStoreAtx(b *testing.B) {
	lg := logtest.New(b)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(b)
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(b, err)
	nid := sig.NodeID()
	atxHdlr := NewHandler(cdb, mclock, mpub, nil, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)

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
				atx := newAtx(b, sig, &nid, challenge, nil, 0, types.Address{})
				vAtx, err := atx.Verify(0, 1)
				if !assert.NoError(b, err) {
					return
				}
				if !assert.NoError(b, atxHdlr.StoreAtx(context.Background(), vAtx)) {
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
	validator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)

	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid := sig.NodeID()
	coinbase := types.Address{2, 4, 5}

	atxHdlr := NewHandler(cdb, mclock, mpub, mockFetch, layersPerEpochBig, testTickSize, goldenATXID, validator, receiver, lg)
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, types.NewLayerID(22), nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	atx1 := newAtx(t, sig, &nid, challenge, nipost, 2, coinbase)
	atx1.PositioningATX = types.ATXID{1, 2, 3} // should be fetched
	atx1.PrevATXID = types.ATXID{4, 5, 6}      // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx1.PositioningATX, atx1.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.Background(), atx1))

	atx2 := newAtx(t, sig, &nid, challenge, nipost, 2, coinbase)
	atx2.PositioningATX = goldenATXID     // should *NOT* be fetched
	atx2.PrevATXID = types.ATXID{2, 3, 4} // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx2.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.Background(), atx2))

	atx3 := newAtx(t, sig, &nid, challenge, nipost, 2, coinbase)
	atx3.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx3.PrevATXID = types.ATXID{3, 4, 5}   // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx3.PrevATXID}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.Background(), atx3))

	atx4 := newAtx(t, sig, &nid, challenge, nipost, 2, coinbase)
	atx4.PositioningATX = types.ATXID{5, 6, 7} // should be fetched
	atx4.PrevATXID = *types.EmptyATXID         // should *NOT* be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx4.PositioningATX}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.Background(), atx4))

	atx5 := newAtx(t, sig, &nid, challenge, nipost, 2, coinbase)
	atx5.PositioningATX = *types.EmptyATXID // should *NOT* be fetched
	atx5.PrevATXID = *types.EmptyATXID      // should *NOT* be fetched
	require.NoError(t, atxHdlr.FetchAtxReferences(context.Background(), atx5))

	atx6 := newAtx(t, sig, &nid, challenge, nipost, 2, coinbase)
	atxid := types.ATXID{1, 2, 3}
	atx6.PositioningATX = atxid // should be fetched
	atx6.PrevATXID = atxid      // should be fetched
	mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxid}).Return(nil)
	require.NoError(t, atxHdlr.FetchAtxReferences(context.Background(), atx6))
}

func TestHandler_AtxWeight(t *testing.T) {
	ctrl := gomock.NewController(t)
	mfetch := mocks.NewMockFetcher(ctrl)
	mvalidator := NewMocknipostValidator(ctrl)
	receiver := NewMockatxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)

	const (
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	handler := NewHandler(cdb, mclock, mpub, mfetch, layersPerEpoch, tickSize, goldenATXID, mvalidator, receiver, lg)

	nonce := types.VRFPostIndex(1)
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
			VRFNonce:    &nonce,
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx1))
	require.NoError(t, atx1.CalcAndSetID())

	buf, err := codec.Encode(atx1)
	require.NoError(t, err)

	mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaves, nil).Times(1)
	mvalidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mvalidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	receiver.EXPECT().OnAtx(gomock.Any()).Times(1)
	require.NoError(t, handler.HandleAtxData(context.Background(), buf))

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
	require.NoError(t, SignAndFinalizeAtx(sig, atx2))
	require.NoError(t, atx2.CalcAndSetID())

	buf, err = codec.Encode(atx2)
	require.NoError(t, err)

	mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any()).Times(1)
	mfetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Times(1)
	mvalidator.EXPECT().NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaves, nil).Times(1)
	mvalidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mvalidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	receiver.EXPECT().OnAtx(gomock.Any()).Times(1)
	require.NoError(t, handler.HandleAtxData(context.Background(), buf))

	stored2, err := cdb.GetAtxHeader(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored2.TickCount)
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.GetWeight()))
}
