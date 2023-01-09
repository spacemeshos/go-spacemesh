package activation

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_Validation_VRFNonce(t *testing.T) {
	r := require.New(t)

	// Arrange
	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	meta := &types.PostMetadata{
		BitsPerLabel:  postCfg.BitsPerLabel,
		LabelsPerUnit: postCfg.LabelsPerUnit,
	}

	initOpts := DefaultPostSetupOpts()
	initOpts.DataDir = t.TempDir()
	initOpts.ComputeProviderID = int(initialization.CPUProviderID())

	nodeId := types.BytesToNodeID(make([]byte, 32))
	commitmentAtxId := types.RandomATXID()

	init, err := initialization.NewInitializer(
		initialization.WithNodeId(nodeId.Bytes()),
		initialization.WithCommitmentAtxId(commitmentAtxId.Bytes()),
		initialization.WithConfig((config.Config)(postCfg)),
		initialization.WithInitOpts((config.InitOpts)(initOpts)),
	)
	r.NoError(err)
	r.NoError(init.Initialize(context.Background()))
	r.NotNil(init.Nonce())

	nonce := (*types.VRFPostIndex)(init.Nonce())

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid vrf nonce", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits))
	})

	t.Run("invalid vrf nonce", func(t *testing.T) {
		t.Parallel()

		nonce := types.VRFPostIndex(1)
		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, &nonce, meta, initOpts.NumUnits))
	})

	t.Run("wrong commitmentAtxId", func(t *testing.T) {
		t.Parallel()

		commitmentAtxId := types.RandomATXID()
		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits))
	})

	t.Run("numUnits can be smaller", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits-1))
	})
}

func Test_Validation_InitialNIPostChallenge(t *testing.T) {
	r := require.New(t)

	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	goldenATXID := types.ATXID{2, 3, 4}

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid initial nipost challenge passes", func(t *testing.T) {
		t.Parallel()

		prevAtxId := types.ATXID{1, 2, 3}
		commitmentAtxId := types.ATXID{5, 6, 7}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, *types.EmptyATXID, prevAtxId, types.NewLayerID(1012), &commitmentAtxId)
		challenge.InitialPostIndices = initialPost.Indices

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(commitmentAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.NewLayerID(888),
			},
		}, nil)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID, initialPost.Indices)
		r.NoError(err)
	})

	t.Run("valid initial nipost challenge in epoch 1 passes", func(t *testing.T) {
		t.Parallel()

		prevAtxId := types.ATXID{1, 2, 3}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, *types.EmptyATXID, prevAtxId, types.NewLayerID(2), &goldenATXID)
		challenge.InitialPostIndices = initialPost.Indices

		atxProvider := NewMockatxProvider(ctrl)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID, initialPost.Indices)
		r.NoError(err)
	})

	t.Run("sequence number is not zero", func(t *testing.T) {
		t.Parallel()

		challenge := &types.NIPostChallenge{
			Sequence: 1,
		}
		err := v.InitialNIPostChallenge(challenge, nil, goldenATXID, nil)
		r.EqualError(err, "no prevATX declared, but sequence number not zero")
	})

	t.Run("missing initial post indices", func(t *testing.T) {
		t.Parallel()
		prevAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(0, *types.EmptyATXID, prevAtxId, types.NewLayerID(2), &goldenATXID)

		err := v.InitialNIPostChallenge(&challenge, nil, goldenATXID, nil)
		r.EqualError(err, "no prevATX declared, but initial Post indices is not included in challenge")
	})

	t.Run("wrong initial post indices", func(t *testing.T) {
		t.Parallel()

		prevAtxId := types.ATXID{1, 2, 3}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, *types.EmptyATXID, prevAtxId, types.NewLayerID(2), &goldenATXID)
		challenge.InitialPostIndices = make([]byte, 10)
		challenge.InitialPostIndices[0] = 1

		err := v.InitialNIPostChallenge(&challenge, nil, goldenATXID, initialPost.Indices)
		r.EqualError(err, "initial Post indices included in challenge does not equal to the initial Post indices included in the atx")
	})

	t.Run("missing commitment atx", func(t *testing.T) {
		t.Parallel()

		prevAtxId := types.ATXID{1, 2, 3}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, *types.EmptyATXID, prevAtxId, types.NewLayerID(2), nil)
		challenge.InitialPostIndices = initialPost.Indices

		err := v.InitialNIPostChallenge(&challenge, nil, goldenATXID, initialPost.Indices)
		r.EqualError(err, "no prevATX declared, but commitmentATX is missing")
	})

	t.Run("commitment atx from wrong pub layer", func(t *testing.T) {
		t.Parallel()

		prevAtxId := types.ATXID{1, 2, 3}
		commitmentAtxId := types.ATXID{5, 6, 7}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, *types.EmptyATXID, prevAtxId, types.NewLayerID(1012), &commitmentAtxId)
		challenge.InitialPostIndices = initialPost.Indices

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(commitmentAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.NewLayerID(1200),
			},
		}, nil)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID, initialPost.Indices)
		r.EqualError(err, "challenge publayer (1012) must be after commitment atx publayer (1200)")
	})
}

// TODO(mafa): add tests for NIPostChallenge, PositioningAtx, Post, NumUnits & NIPost
