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

	"github.com/spacemeshos/merkle-tree"
	poetShared "github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpochBig = 1000
	localID           = "local"
)

func newMerkleProof(t testing.TB, challenge types.Hash32, otherLeafs []types.Hash32) (types.MerkleProof, types.Hash32) {
	t.Helper()
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(poetShared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	require.NoError(t, err)
	require.NoError(t, tree.AddLeaf(challenge[:]))
	for _, m := range otherLeafs {
		require.NoError(t, tree.AddLeaf(m[:]))
	}
	root, nodes := tree.RootAndProof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return types.MerkleProof{
		Nodes: nodesH32,
	}, types.BytesToHash(root)
}

func newNIPostWithChallenge(t testing.TB, challenge types.Hash32, poetRef []byte) *types.NIPost {
	t.Helper()
	proof, _ := newMerkleProof(t, challenge, []types.Hash32{
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
		types.BytesToHash([]byte("leaf4")),
	})

	return &types.NIPost{
		Membership: proof,
		Post: &types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: &types.PostMetadata{
			Challenge: poetRef,
		},
	}
}

type testHandler struct {
	*Handler

	mclock     *MocklayerClock
	mpub       *pubsubmocks.MockPublisher
	mockFetch  *mocks.MockFetcher
	mValidator *MocknipostValidator
	mbeacon    *MockAtxReceiver
	mtortoise  *mocks.MockTortoise
}

func newTestHandler(tb testing.TB, goldenATXID types.ATXID) *testHandler {
	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(tb)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	mockFetch := mocks.NewMockFetcher(ctrl)
	mValidator := NewMocknipostValidator(ctrl)

	mbeacon := NewMockAtxReceiver(ctrl)
	mtortoise := mocks.NewMockTortoise(ctrl)

	atxHdlr := NewHandler(
		localID,
		cdb,
		atxsdata.New(),
		signing.NewEdVerifier(),
		mclock,
		mpub,
		mockFetch,
		1,
		goldenATXID,
		mValidator,
		mbeacon,
		mtortoise,
		lg,
		PoetConfig{},
	)
	return &testHandler{
		Handler: atxHdlr,

		mclock:     mclock,
		mpub:       mpub,
		mockFetch:  mockFetch,
		mValidator: mValidator,
		mbeacon:    mbeacon,
		mtortoise:  mtortoise,
	}
}

func TestHandler_processBlockATXs(t *testing.T) {
	// Arrange
	r := require.New(t)

	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any()).AnyTimes()
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any()).AnyTimes()

	sig, err := signing.NewEdSigner()
	r.NoError(err)
	sig1, err := signing.NewEdSigner()
	r.NoError(err)
	sig2, err := signing.NewEdSigner()
	r.NoError(err)
	sig3, err := signing.NewEdSigner()
	r.NoError(err)

	coinbase1 := types.GenerateAddress([]byte("aaaa"))
	coinbase2 := types.GenerateAddress([]byte("bbbb"))
	coinbase3 := types.GenerateAddress([]byte("cccc"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	numTicks := uint64(100)
	numUnits := uint32(100)

	npst := newNIPostWithChallenge(t, chlng, poetRef)
	posATX := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.LayerID(1000).GetEpoch(),
		0,
		numTicks,
		coinbase1,
		numUnits,
		npst,
	)
	r.NoError(atxs.Add(atxHdlr.cdb, posATX))

	// Act
	atxList := []*types.VerifiedActivationTx{
		newActivationTx(
			t,
			sig1,
			0,
			types.EmptyATXID,
			posATX.ID(),
			nil,
			types.LayerID(1012).GetEpoch(),
			0,
			numTicks,
			coinbase1,
			numUnits,
			&types.NIPost{},
		),
		newActivationTx(
			t,
			sig2,
			0,
			types.EmptyATXID,
			posATX.ID(),
			nil,
			types.LayerID(1300).GetEpoch(),
			0,
			numTicks,
			coinbase2,
			numUnits,
			&types.NIPost{},
		),
		newActivationTx(
			t,
			sig3,
			0,
			types.EmptyATXID,
			posATX.ID(),
			nil,
			types.LayerID(1435).GetEpoch(),
			0,
			numTicks,
			coinbase3,
			numUnits,
			&types.NIPost{},
		),
	}
	for _, atx := range atxList {
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		r.NoError(atxHdlr.ProcessAtx(context.Background(), atx))
	}

	// check that further atxList don't affect current epoch count
	atxList2 := []*types.VerifiedActivationTx{
		newActivationTx(
			t,
			sig1,
			1,
			atxList[0].ID(),
			atxList[0].ID(),
			nil,
			types.LayerID(2012).GetEpoch(),
			0,
			numTicks,
			coinbase1,
			numUnits,
			&types.NIPost{},
		),
		newActivationTx(
			t,
			sig2,
			1,
			atxList[1].ID(),
			atxList[1].ID(),
			nil,
			types.LayerID(2300).GetEpoch(),
			0,
			numTicks,
			coinbase2,
			numUnits,
			&types.NIPost{},
		),
		newActivationTx(
			t,
			sig3,
			1,
			atxList[2].ID(),
			atxList[2].ID(),
			nil,
			types.LayerID(2435).GetEpoch(),
			0,
			numTicks,
			coinbase3,
			numUnits,
			&types.NIPost{},
		),
	}
	for _, atx := range atxList2 {
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		r.NoError(atxHdlr.ProcessAtx(context.Background(), atx))
	}

	// Assert
	// 1 posATX + 3 atx from `atxList`; weight = 4 * numTicks * numUnit
	epochWeight, _, err := atxHdlr.cdb.GetEpochWeight(2)
	r.NoError(err)
	r.Equal(4*numTicks*uint64(numUnits), epochWeight)

	// 3 atx from `atxList2`; weight = 3 * numTicks * numUnit
	epochWeight, _, err = atxHdlr.cdb.GetEpochWeight(3)
	r.NoError(err)
	r.Equal(3*numTicks*uint64(numUnits), epochWeight)
}

func TestHandler_SyntacticallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	goldenATXID := types.ATXID{2, 3, 4}
	currentLayer := types.LayerID(1012)

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := newNIPostWithChallenge(t, chlng, poetRef)
	prevAtx := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		&goldenATXID,
		currentLayer.GetEpoch()-1,
		0,
		100,
		types.GenerateAddress([]byte("aaaa")),
		100,
		npst,
	)
	nonce := types.VRFPostIndex(1)
	prevAtx.VRFNonce = &nonce
	posAtx := newActivationTx(
		t,
		otherSig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		&goldenATXID,
		currentLayer.GetEpoch()-1,
		0,
		100,
		types.GenerateAddress([]byte("bbbb")),
		100,
		npst,
	)

	// Act & Assert
	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("valid atx with new VRF nonce", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		nonce := types.VRFPostIndex(999)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		atx.VRFNonce = &nonce
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)

		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("valid atx with decreasing num units", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(
			t,
			sig,
			challenge,
			&types.NIPost{},
			90,
			types.GenerateAddress([]byte("aaaa")),
		) // numunits decreased from 100 to 90 between atx and prevAtx
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		vAtx, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint32(90), vAtx.NumUnits)
		require.Equal(t, uint32(90), vAtx.EffectiveNumUnits())
	})

	t.Run("atx with increasing num units, no new VRF, old valid", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(
			t,
			sig,
			challenge,
			&types.NIPost{},
			110,
			types.GenerateAddress([]byte("aaaa")),
		) // numunits increased from 100 to 110 between atx and prevAtx
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		vAtx, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint32(110), vAtx.NumUnits)
		require.Equal(t, uint32(100), vAtx.EffectiveNumUnits())
	})

	t.Run("atx with increasing num units, no new VRF, old invalid for new size", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(
			t,
			sig,
			challenge,
			&types.NIPost{},
			110,
			types.GenerateAddress([]byte("aaaa")),
		) // numunits increased from 100 to 110 between atx and prevAtx
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("invalid VRF"))
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "invalid vrf nonce")
	})

	t.Run("valid initial atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, posAtx))

		ctxID := posAtx.ID()
		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), &ctxID)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
	})

	t.Run("atx targeting wrong publish epoch", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch().Add(2), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx publish epoch is too far in the future")
	})

	t.Run("failing nipost challenge validation", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("nipost error"))
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "nipost error")
	})

	t.Run("failing positioning atx validation", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(prevAtx.Sequence+1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("bad positioning atx"))
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad positioning atx")
	})

	t.Run("bad initial nipost challenge", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		cATX := posAtx.ID()
		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), &cATX)
		atx := newAtx(t, sig, challenge, npst, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		vrfNonce := types.VRFPostIndex(11)
		atx.VRFNonce = &vrfNonce
		atx.SmesherID = sig.NodeID()

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().Post(gomock.Any(), sig.NodeID(), cATX, atx.InitialPost, gomock.Any(), atx.NumUnits)
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), cATX, &vrfNonce, gomock.Any(), atx.NumUnits)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("bad initial nipost"))
		_, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad initial nipost")
	})

	t.Run("missing NodeID in initial atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		ctxID := posAtx.ID()
		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), &ctxID)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "node id is missing")
	})

	t.Run("missing VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		cATX := posAtx.ID()
		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), &cATX)
		atx := newAtx(t, sig, challenge, npst, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		nodeID := sig.NodeID()
		atx.InnerActivationTx.NodeID = &nodeID

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "vrf nonce is missing")
	})

	t.Run("invalid VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		ctxID := posAtx.ID()
		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), &ctxID)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("invalid VRF nonce"))
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF nonce")
	})

	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.CommitmentATX = &goldenATXID

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err = atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but initial post is not included")
	})

	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		cATX := posAtx.ID()
		challenge := newChallenge(0, types.EmptyATXID, posAtx.ID(), currentLayer.GetEpoch(), &cATX)
		atx := newAtx(t, sig, challenge, npst, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("failed post validation"))
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "failed post validation")
	})

	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but initial post is included")
	})

	t.Run("prevAtx declared but NodeID is included", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(1, prevAtx.ID(), prevAtx.ID(), currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithChallenge(t, atx.NIPostChallenge.Hash(), poetRef)
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but node id is included")
	})
}

func TestHandler_ContextuallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	goldenATXID := types.ATXID{2, 3, 4}
	currentLayer := types.LayerID(1012)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := newNIPostWithChallenge(t, chlng, poetRef)
	prevAtx := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		currentLayer.GetEpoch()-1,
		0,
		100,
		coinbase,
		100,
		npst,
	)

	// Act & Assert

	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := newChallenge(1, types.EmptyATXID, goldenATXID, 0, nil)
		atx := newAtx(t, sig, challenge, nil, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.ContextuallyValidateAtx(vAtx))
	})

	t.Run("atx already exists", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := newChallenge(1, types.EmptyATXID, goldenATXID, currentLayer.GetEpoch(), nil)
		atx := newAtx(t, sig, challenge, &types.NIPost{}, 100, coinbase)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(atxHdlr.cdb, vAtx))

		challenge = newChallenge(1, prevAtx.ID(), goldenATXID, currentLayer.GetEpoch()-1, nil)
		atx = newAtx(t, sig, challenge, &types.NIPost{}, 100, coinbase)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		vAtx, err = atx.Verify(0, 1)
		require.NoError(t, err)

		err = atxHdlr.ContextuallyValidateAtx(vAtx)
		require.EqualError(t, err, "last atx is not the one referenced")
	})

	t.Run("missing prevAtx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := newChallenge(1, types.RandomATXID(), prevAtx.ID(), 0, nil)
		atx := newAtx(t, sig, challenge, nil, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)

		err = atxHdlr.ContextuallyValidateAtx(vAtx)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx by same node", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		vAtx := newActivationTx(
			t,
			sig,
			prevAtx.Sequence+1,
			prevAtx.ID(),
			prevAtx.ID(),
			nil,
			currentLayer.GetEpoch(),
			0,
			100,
			coinbase,
			100,
			&types.NIPost{},
		)
		require.NoError(t, atxs.Add(atxHdlr.cdb, vAtx))

		vAtx2 := newActivationTx(
			t,
			sig,
			vAtx.Sequence+1,
			prevAtx.ID(),
			prevAtx.ID(),
			nil,
			vAtx.PublishEpoch+1,
			0,
			100,
			coinbase,
			100,
			&types.NIPost{},
		)
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx2), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, vAtx.ID(), id)
		_, err = atxHdlr.cdb.GetAtxHeader(vAtx2.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx from different node", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		otherAtx := newActivationTx(
			t,
			otherSig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			currentLayer.GetEpoch()-1,
			0,
			100,
			coinbase,
			100,
			npst,
		)
		require.NoError(t, atxs.Add(atxHdlr.cdb, otherAtx))

		vAtx := newActivationTx(
			t,
			sig,
			2,
			otherAtx.ID(),
			otherAtx.ID(),
			nil,
			currentLayer.GetEpoch(),
			0,
			100,
			coinbase,
			100,
			&types.NIPost{},
		)
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, prevAtx.ID(), id)
		_, err = atxHdlr.cdb.GetAtxHeader(vAtx.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
}

func TestHandler_ProcessAtx(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.LayerID(layersPerEpoch).GetEpoch(),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{},
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx1))

	// processing an already stored ATX returns no error
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx1))
	proof, err := identities.GetMalfeasanceProof(atxHdlr.cdb, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, proof)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(
		t,
		sig,
		1,
		atx1.ID(),
		atx1.ID(),
		nil,
		types.LayerID(layersPerEpoch+1).GetEpoch(),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{},
	)
	var got types.MalfeasanceGossip
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())
	atxHdlr.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			require.NoError(t, codec.Decode(data, &got))
			nodeID, err := malfeasance.Validate(
				context.Background(),
				atxHdlr.log,
				atxHdlr.cdb,
				atxHdlr.edVerifier,
				&got,
			)
			require.NoError(t, err)
			require.Equal(t, sig.NodeID(), nodeID)
			return nil
		})
	require.ErrorIs(t, atxHdlr.ProcessAtx(context.Background(), atx2), errMaliciousATX)
	proof, err = identities.GetMalfeasanceProof(atxHdlr.cdb, sig.NodeID())
	require.NoError(t, err)
	require.NotNil(t, proof.Received())
	proof.SetReceived(time.Time{})
	require.Equal(t, got.MalfeasanceProof, *proof)
	require.Equal(t, atx2.PublishEpoch.FirstLayer(), got.MalfeasanceProof.Layer)
}

func TestHandler_ProcessAtxStoresNewVRFNonce(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.LayerID(layersPerEpoch).GetEpoch(),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{},
	)
	nonce1 := types.VRFPostIndex(123)
	atx1.VRFNonce = &nonce1
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx1))

	got, err := atxs.VRFNonce(atxHdlr.cdb, sig.NodeID(), atx1.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(
		t,
		sig,
		1,
		atx1.ID(),
		atx1.ID(),
		nil,
		types.LayerID(2*layersPerEpoch).GetEpoch(),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{},
	)
	nonce2 := types.VRFPostIndex(456)
	atx2.VRFNonce = &nonce2
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx2))

	got, err = atxs.VRFNonce(atxHdlr.cdb, sig.NodeID(), atx2.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce2, got)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(b, goldenATXID)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())

	const (
		numOfMiners = 300
		batchSize   = 15
		numOfEpochs = 10 * batchSize
	)
	prevAtxs := make([]types.ATXID, numOfMiners)
	pPrevAtxs := make([]types.ATXID, numOfMiners)
	posAtx := types.ATXID{2, 3, 4}
	var atx *types.ActivationTx
	layer := types.LayerID(22)
	poetBytes := []byte("66666")
	coinbase := types.Address{2, 4, 5}
	sigs := make([]*signing.EdSigner, 0, numOfMiners)
	for i := 0; i < numOfMiners; i++ {
		sig, err := signing.NewEdSigner()
		r.NoError(err)
		sigs = append(sigs, sig)
	}

	start := time.Now()
	eStart := time.Now()
	for epoch := postGenesisEpoch; epoch < postGenesisEpoch+numOfEpochs; epoch++ {
		for i := 0; i < numOfMiners; i++ {
			challenge := newChallenge(1, prevAtxs[i], posAtx, epoch, nil)
			npst := newNIPostWithChallenge(b, challenge.Hash(), poetBytes)
			atx = newAtx(b, sigs[i], challenge, npst, 2, coinbase)
			SignAndFinalizeAtx(sigs[i], atx)
			vAtx, err := atx.Verify(0, 1)
			r.NoError(err)
			require.NoError(b, atxHdlr.ProcessAtx(context.Background(), vAtx))
			prevAtxs[i] = atx.ID()
		}
		posAtx = atx.ID()
		layer = layer.Add(layersPerEpoch)
		if epoch%batchSize == batchSize-1 {
			b.Logf("epoch %3d-%3d took %v\t", epoch-(batchSize-1), epoch, time.Since(eStart))
			eStart = time.Now()

			for miner := 0; miner < numOfMiners; miner++ {
				atx, err := atxHdlr.cdb.GetAtxHeader(prevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
				atx, err = atxHdlr.cdb.GetAtxHeader(pPrevAtxs[miner])
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

func TestHandler_HandleGossipAtx(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID1 := sig1.NodeID()
	nipost := newNIPostWithChallenge(t, types.HexToHash32("0x3333"), []byte{0xba, 0xbe})
	vrfNonce := types.VRFPostIndex(12345)
	first := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   1,
				Sequence:       0,
				PrevATXID:      types.EmptyATXID,
				PositioningATX: goldenATXID,
				CommitmentATX:  &goldenATXID,
				InitialPost:    nipost.Post,
			},
			Coinbase: types.Address{2, 3, 4},
			NumUnits: 2,
			NIPost:   nipost,
			NodeID:   &nodeID1,
			VRFNonce: &vrfNonce,
		},
		SmesherID: nodeID1,
	}
	first.Signature = sig1.Sign(signing.ATX, first.SignedBytes())
	first.SetEffectiveNumUnits(first.NumUnits)
	first.SetReceived(time.Now())
	_, err = first.Verify(0, 2)
	require.NoError(t, err)

	second := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   2,
				Sequence:       1,
				PrevATXID:      first.ID(),
				PositioningATX: first.ID(),
			},
			Coinbase: types.Address{2, 3, 4},
			NumUnits: 2,
			NIPost:   nipost,
		},
		SmesherID: nodeID1,
	}
	second.Signature = sig1.Sign(signing.ATX, second.SignedBytes())
	secondData, err := codec.Encode(second)
	require.NoError(t, err)

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(second.PublishEpoch.FirstLayer())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), second.GetPoetProofRef()).Return(errors.New("woof"))
	err = atxHdlr.HandleGossipAtx(context.Background(), "", secondData)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing poet proof")

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(second.PublishEpoch.FirstLayer())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), second.GetPoetProofRef())
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []types.ATXID) error {
			require.ElementsMatch(t, []types.ATXID{first.ID()}, ids)
			data, err := codec.Encode(first)
			require.NoError(t, err)
			atxHdlr.mclock.EXPECT().CurrentLayer().Return(first.PublishEpoch.FirstLayer())
			atxHdlr.mValidator.EXPECT().
				Post(gomock.Any(), nodeID1, goldenATXID, first.InitialPost, gomock.Any(), first.NumUnits)
			atxHdlr.mValidator.EXPECT().VRFNonce(nodeID1, goldenATXID, &vrfNonce, gomock.Any(), first.NumUnits)
			atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
			atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), first.GetPoetProofRef())
			atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(&first.NIPostChallenge, gomock.Any(), goldenATXID)
			atxHdlr.mValidator.EXPECT().PositioningAtx(goldenATXID, gomock.Any(), goldenATXID, first.PublishEpoch)
			atxHdlr.mValidator.EXPECT().
				NIPost(gomock.Any(), nodeID1, goldenATXID, second.NIPost, gomock.Any(), second.NumUnits)
			atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
			atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
			require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", data))
			return nil
		},
	)
	atxHdlr.mValidator.EXPECT().NIPostChallenge(&second.NIPostChallenge, gomock.Any(), nodeID1)
	atxHdlr.mValidator.EXPECT().NIPost(gomock.Any(), nodeID1, goldenATXID, second.NIPost, gomock.Any(), second.NumUnits)
	atxHdlr.mValidator.EXPECT().PositioningAtx(second.PositioningATX, gomock.Any(), goldenATXID, second.PublishEpoch)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", secondData))
}

func TestHandler_HandleParallelGossipAtx(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := sig.NodeID()
	nipost := newNIPostWithChallenge(t, types.HexToHash32("0x3333"), []byte{0xba, 0xbe})
	vrfNonce := types.VRFPostIndex(12345)
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   1,
				PrevATXID:      types.EmptyATXID,
				PositioningATX: goldenATXID,
				CommitmentATX:  &goldenATXID,
				InitialPost:    nipost.Post,
			},
			Coinbase: types.Address{2, 3, 4},
			NumUnits: 2,
			NIPost:   nipost,
			NodeID:   &nodeID,
			VRFNonce: &vrfNonce,
		},
		SmesherID: nodeID,
	}
	atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	_, err = atx.Verify(0, 2)
	require.NoError(t, err)

	atxData, err := codec.Encode(atx)
	require.NoError(t, err)

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
	atxHdlr.mValidator.EXPECT().VRFNonce(nodeID, goldenATXID, &vrfNonce, gomock.Any(), atx.NumUnits)
	atxHdlr.mValidator.EXPECT().Post(
		gomock.Any(),
		atx.SmesherID,
		goldenATXID,
		atx.InitialPost,
		gomock.Any(),
		atx.NumUnits,
	).DoAndReturn(
		func(_ context.Context, _ types.NodeID, _ types.ATXID, _ *types.Post, _ *types.PostMetadata, _ uint32) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		},
	)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(&atx.NIPostChallenge, gomock.Any(), goldenATXID)
	atxHdlr.mValidator.EXPECT().PositioningAtx(goldenATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
	atxHdlr.mValidator.EXPECT().NIPost(gomock.Any(), nodeID, goldenATXID, atx.NIPost, gomock.Any(), atx.NumUnits)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())

	var eg errgroup.Group
	for i := 0; i < 10; i++ {
		eg.Go(func() error {
			return atxHdlr.HandleGossipAtx(context.Background(), "", atxData)
		})
	}

	require.NoError(t, eg.Wait())
}

func TestHandler_HandleSyncedAtx(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Act & Assert

	t.Run("missing nipost", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			nil,
		)
		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.ErrorContains(
			t,
			atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf),
			fmt.Sprintf("nil nipst for atx %v", atx.ID()),
		)
	})

	t.Run("known atx is ignored by handleAtxData", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			nil,
		)

		atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
		require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx))

		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))

		require.Error(t, atxHdlr.HandleGossipAtx(context.Background(), "", buf))
	})
	t.Run("known atx from local id is allowed", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			nil,
		)

		atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
		require.NoError(t, atxHdlr.ProcessAtx(context.Background(), atx))

		buf, err := codec.Encode(atx)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), localID, buf))
	})

	t.Run("atx with invalid signature", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			nil,
		)
		atx.Signature[0] = ^atx.Signature[0] // fip first 8 bits
		buf, err := codec.Encode(atx)
		require.NoError(t, err)

		require.ErrorContains(
			t,
			atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf),
			"failed to verify atx signature",
		)
	})
}

func BenchmarkGetAtxHeaderWithConcurrentProcessAtx(b *testing.B) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(b, goldenATXID)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())

	var (
		stop uint64
		wg   sync.WaitGroup
	)
	for i := 0; i < runtime.NumCPU()/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				challenge := newChallenge(uint64(i), types.EmptyATXID, goldenATXID, 0, nil)
				sig, err := signing.NewEdSigner()
				require.NoError(b, err)
				atx := newAtx(b, sig, challenge, nil, 1, types.Address{})
				require.NoError(b, SignAndFinalizeAtx(sig, atx))
				vAtx, err := atx.Verify(0, 1)
				if !assert.NoError(b, err) {
					return
				}
				if !assert.NoError(b, atxHdlr.ProcessAtx(context.Background(), vAtx)) {
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

// Check that we're not trying to sync an ATX that references the golden ATX or an empty ATX
// (i.e. not adding it to the sync queue).
func TestHandler_FetchReferences(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	posATX := types.ATXID{1, 2, 3}
	prevATX := types.ATXID{4, 5, 6}
	commitATX := types.ATXID{7, 8, 9}

	t.Run("valid prev and pos ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, prevATX, posATX, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().
			GetAtxs(gomock.Any(), gomock.InAnyOrder([]types.ATXID{atx.PositioningATX, atx.PrevATXID})).
			Return(nil)
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("valid prev ATX and golden pos ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, prevATX, goldenATXID, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx.PrevATXID}).Return(nil)
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("valid prev ATX and empty pos ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, prevATX, types.EmptyATXID, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx.PrevATXID}).Return(nil)
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("empty prev ATX, valid pos ATX and valid commitment ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, types.EmptyATXID, posATX, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)
		atx.CommitmentATX = &commitATX

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().
			GetAtxs(gomock.Any(), gomock.InAnyOrder([]types.ATXID{atx.PositioningATX, *atx.CommitmentATX})).
			Return(nil)
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("empty prev ATX, valid pos ATX and golden commitment ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, types.EmptyATXID, posATX, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)
		atx.CommitmentATX = &goldenATXID

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx.PositioningATX}).Return(nil)
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("empty prev ATX, empty pos ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, types.EmptyATXID, types.EmptyATXID, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("same prev and pos ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, prevATX, prevATX, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx.PrevATXID}).Return(nil)
		require.NoError(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("no poet proofs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, types.EmptyATXID, types.EmptyATXID, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef()).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.FetchReferences(context.Background(), atx))
	})

	t.Run("no atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		coinbase := types.Address{2, 4, 5}
		challenge := newChallenge(1, prevATX, prevATX, types.LayerID(22).GetEpoch(), nil)
		nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
		atx := newAtx(t, sig, challenge, nipost, 2, coinbase)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx.PrevATXID}).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.FetchReferences(context.Background(), atx))
	})
}

func TestHandler_AtxWeight(t *testing.T) {
	const (
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)
	atxHdlr.tickSize = tickSize

	currentLayer := types.LayerID(100)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce := types.VRFPostIndex(1)
	nodeId := sig.NodeID()
	atx1 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PositioningATX: goldenATXID,
				InitialPost:    &types.Post{Indices: []byte{1}},
				PublishEpoch:   1,
				CommitmentATX:  &goldenATXID,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			VRFNonce: &nonce,
			NodeID:   &nodeId,
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx1))

	buf, err := codec.Encode(atx1)
	require.NoError(t, err)

	peer := p2p.Peer("buddy")
	proofRef := atx1.GetPoetProofRef()
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{proofRef})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(leaves, nil)
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx1.ID().Hash32(), peer, buf))

	stored1, err := atxHdlr.cdb.GetAtxHeader(atx1.ID())
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
				PublishEpoch:   2,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx2))

	buf, err = codec.Encode(atx2)
	require.NoError(t, err)

	proofRef = atx2.GetPoetProofRef()
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, gomock.Any()).Do(
		func(_ p2p.Peer, got []types.Hash32) {
			require.ElementsMatch(t, []types.Hash32{atx1.ID().Hash32(), proofRef}, got)
		})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(leaves, nil)
	atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any())
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx2.ID().Hash32(), peer, buf))

	stored2, err := atxHdlr.cdb.GetAtxHeader(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored2.TickCount)
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.GetWeight()))
}

func TestHandler_WrongHash(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)
	currentLayer := types.LayerID(100)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce := types.VRFPostIndex(1)
	nodeId := sig.NodeID()
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PositioningATX: goldenATXID,
				InitialPost:    &types.Post{Indices: []byte{1}},
				PublishEpoch:   1,
				CommitmentATX:  &goldenATXID,
			},
			NumUnits: 3,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			VRFNonce: &nonce,
			NodeID:   &nodeId,
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx))

	buf, err := codec.Encode(atx)
	require.NoError(t, err)

	peer := p2p.Peer("buddy")
	proofRef := atx.GetPoetProofRef()
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{proofRef})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uint64(111), nil)
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	err = atxHdlr.HandleSyncedAtx(context.Background(), types.RandomHash(), peer, buf)
	require.ErrorIs(t, err, errWrongHash)
	require.ErrorIs(t, err, pubsub.ErrValidationReject)
}
