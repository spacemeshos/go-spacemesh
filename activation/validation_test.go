package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_Validation_VRFNonce(t *testing.T) {
	r := require.New(t)

	// Arrange
	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	postCfg.LabelsPerUnit = 128
	meta := &types.PostMetadata{
		LabelsPerUnit: postCfg.LabelsPerUnit,
	}

	initOpts := DefaultPostSetupOpts()
	initOpts.DataDir = t.TempDir()
	initOpts.ProviderID = int(initialization.CPUProviderID())

	nodeId := types.BytesToNodeID(make([]byte, 32))
	commitmentAtxId := types.EmptyATXID

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

		commitmentAtxId := types.ATXID{1, 2, 3}
		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits))
	})

	t.Run("numUnits can be smaller", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits-1))
	})
}

func Test_Validation_InitialNIPostChallenge(t *testing.T) {
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

		posAtxId := types.ATXID{1, 2, 3}
		commitmentAtxId := types.ATXID{5, 6, 7}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, types.EmptyATXID, posAtxId, 2, &commitmentAtxId)
		challenge.InitialPostIndices = initialPost.Indices

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(commitmentAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
			},
		}, nil)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID, initialPost.Indices)
		require.NoError(t, err)
	})

	t.Run("valid initial nipost challenge in epoch 1 passes", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, types.EmptyATXID, posAtxId, types.LayerID(2).GetEpoch(), &goldenATXID)
		challenge.InitialPostIndices = initialPost.Indices

		atxProvider := NewMockatxProvider(ctrl)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID, initialPost.Indices)
		require.NoError(t, err)
	})

	t.Run("sequence number is not zero", func(t *testing.T) {
		t.Parallel()

		challenge := &types.NIPostChallenge{
			Sequence: 1,
		}
		err := v.InitialNIPostChallenge(challenge, nil, goldenATXID, nil)
		require.EqualError(t, err, "no prevATX declared, but sequence number not zero")
	})

	t.Run("missing initial post indices", func(t *testing.T) {
		t.Parallel()
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(0, types.EmptyATXID, posAtxId, types.LayerID(2).GetEpoch(), &goldenATXID)

		err := v.InitialNIPostChallenge(&challenge, nil, goldenATXID, nil)
		require.EqualError(t, err, "no prevATX declared, but initial Post indices is not included in challenge")
	})

	t.Run("wrong initial post indices", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, types.EmptyATXID, posAtxId, types.LayerID(2).GetEpoch(), &goldenATXID)
		challenge.InitialPostIndices = make([]byte, 10)
		challenge.InitialPostIndices[0] = 1

		err := v.InitialNIPostChallenge(&challenge, nil, goldenATXID, initialPost.Indices)
		require.EqualError(t, err, "initial Post indices included in challenge does not equal to the initial Post indices included in the atx")
	})

	t.Run("missing commitment atx", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, types.EmptyATXID, posAtxId, types.LayerID(2).GetEpoch(), nil)
		challenge.InitialPostIndices = initialPost.Indices

		err := v.InitialNIPostChallenge(&challenge, nil, goldenATXID, initialPost.Indices)
		require.EqualError(t, err, "no prevATX declared, but commitmentATX is missing")
	})

	t.Run("commitment atx from wrong pub layer", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		commitmentAtxId := types.ATXID{5, 6, 7}
		initialPost := &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		challenge := newChallenge(0, types.EmptyATXID, posAtxId, 1, &commitmentAtxId)
		challenge.InitialPostIndices = initialPost.Indices

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(commitmentAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 2,
			},
		}, nil)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID, initialPost.Indices)
		require.EqualError(t, err, "challenge pubepoch (1) must be after commitment atx pubepoch (2)")
	})
}

func Test_Validation_NIPostChallenge(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid nipost challenge passes", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, 2, nil)

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
				Sequence:     9,
			},
			NodeID: nodeId,
		}, nil)

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.NoError(t, err)
	})

	t.Run("prev atx missing", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, types.LayerID(1012).GetEpoch(), nil)

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(nil, errors.New("not found"))

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.ErrorIs(t, err, &ErrAtxNotFound{Id: prevAtxId})
		require.ErrorContains(t, err, "not found")
	})

	t.Run("prev ATX from different miner", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()
		otherNodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, types.LayerID(1012).GetEpoch(), nil)

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: types.EpochID(888),
				Sequence:     9,
			},
			NodeID: otherNodeId,
		}, nil)

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.ErrorContains(t, err, "previous atx belongs to different miner")
	})

	t.Run("prev atx from wrong publication epoch", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, 2, nil)

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 3,
				Sequence:     9,
			},
			NodeID: nodeId,
		}, nil)

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.EqualError(t, err, "prevAtx epoch (3) isn't older than current atx epoch (2)")
	})

	t.Run("prev atx sequence not lower than current", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, 2, nil)

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
				Sequence:     10,
			},
			NodeID: nodeId,
		}, nil)

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.EqualError(t, err, "sequence number is not one more than prev sequence number")
	})

	t.Run("challenge contains initial post indices", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, 2, nil)
		challenge.InitialPostIndices = make([]byte, 10)

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
				Sequence:     9,
			},
			NodeID: nodeId,
		}, nil)

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.EqualError(t, err, "prevATX declared, but initial Post indices is included in challenge")
	})

	t.Run("challenge contains commitment atx", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := newChallenge(10, prevAtxId, posAtxId, 2, nil)
		challenge.CommitmentATX = &types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(prevAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
				Sequence:     9,
			},
			NodeID: nodeId,
		}, nil)

		err := v.NIPostChallenge(&challenge, atxProvider, nodeId)
		require.EqualError(t, err, "prevATX declared, but commitmentATX is included")
	})
}

func Test_Validation_PositioningAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid nipost challenge passes", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(posAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
				Sequence:     9,
			},
		}, nil)

		err := v.PositioningAtx(&posAtxId, atxProvider, goldenAtxId, 2, layersPerEpochBig)
		require.NoError(t, err)
	})

	t.Run("golden ATX is allowed as positioning atx in genesis epoch", func(t *testing.T) {
		t.Parallel()

		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.PositioningAtx(&goldenAtxId, atxProvider, goldenAtxId, types.LayerID(1012).GetEpoch(), layersPerEpochBig)
		require.NoError(t, err)
	})

	t.Run("golden ATX is allowed as positioning atx in non-genesis epoch", func(t *testing.T) {
		t.Parallel()

		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.PositioningAtx(&goldenAtxId, atxProvider, goldenAtxId, 5, layersPerEpochBig)
		require.NoError(t, err)
	})

	t.Run("fail at empty positioning atx", func(t *testing.T) {
		t.Parallel()

		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.PositioningAtx(&types.EmptyATXID, atxProvider, goldenAtxId, types.LayerID(1012).GetEpoch(), layersPerEpochBig)
		require.EqualError(t, err, "empty positioning atx")
	})

	t.Run("fail when posAtx is not found", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(posAtxId).Return(nil, errors.New("db error"))

		err := v.PositioningAtx(&posAtxId, atxProvider, goldenAtxId, types.LayerID(1012).GetEpoch(), layersPerEpochBig)
		require.ErrorIs(t, err, &ErrAtxNotFound{Id: posAtxId})
		require.ErrorContains(t, err, "db error")
	})

	t.Run("positioning atx published in higher epoch than expected", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(posAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 5,
				Sequence:     9,
			},
		}, nil)

		err := v.PositioningAtx(&posAtxId, atxProvider, goldenAtxId, 3, layersPerEpochBig)
		require.EqualError(t, err, "positioning atx epoch (5) must be before 3")
	})

	t.Run("any distance to positioning atx is valid", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(posAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
				Sequence:     9,
			},
		}, nil)

		err := v.PositioningAtx(&posAtxId, atxProvider, goldenAtxId, 10, layersPerEpochBig)
		require.NoError(t, err)
	})
}

func Test_Validate_NumUnits(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid number of num units passes", func(t *testing.T) {
		t.Parallel()

		err := v.NumUnits(&postCfg, postCfg.MinNumUnits)
		require.NoError(t, err)

		err = v.NumUnits(&postCfg, postCfg.MaxNumUnits)
		require.NoError(t, err)
	})

	t.Run("invalid number of num units fails", func(t *testing.T) {
		t.Parallel()

		err := v.NumUnits(&postCfg, postCfg.MinNumUnits-1)
		require.EqualError(t, err, fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", postCfg.MinNumUnits, postCfg.MinNumUnits-1))

		err = v.NumUnits(&postCfg, postCfg.MaxNumUnits+1)
		require.EqualError(t, err, fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", postCfg.MaxNumUnits, postCfg.MaxNumUnits+1))
	})
}

func Test_Validate_PostMetadata(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid post metadata", func(t *testing.T) {
		t.Parallel()

		meta := &types.PostMetadata{
			LabelsPerUnit: postCfg.LabelsPerUnit,
		}

		err := v.PostMetadata(&postCfg, meta)
		require.NoError(t, err)
	})

	t.Run("wrong labels per unit", func(t *testing.T) {
		t.Parallel()

		meta := &types.PostMetadata{
			LabelsPerUnit: postCfg.LabelsPerUnit - 1,
		}

		err := v.PostMetadata(&postCfg, meta)
		require.EqualError(t, err, fmt.Sprintf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", postCfg.LabelsPerUnit, postCfg.LabelsPerUnit-1))
	})
}

func TestValidator_Validate(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	challengeHash := challenge.Hash()
	poetDb := NewMockpoetDbAPI(gomock.NewController(t))
	poetDb.EXPECT().GetProof(gomock.Any()).AnyTimes().Return(&types.PoetProof{}, nil)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)

	postProvider := newTestPostManager(t)
	nipost := buildNIPost(t, postProvider, postProvider.cfg, challenge, poetDb)

	opts := []verifying.OptionFunc{verifying.WithLabelScryptParams(postProvider.opts.Scrypt)}
	err := validateNIPost(postProvider.id, postProvider.commitmentAtxId, nipost, challengeHash, poetDb, postProvider.cfg, postProvider.opts.NumUnits, opts...)
	r.NoError(err)

	err = validateNIPost(postProvider.id, postProvider.commitmentAtxId, nipost, types.BytesToHash([]byte("lerner")), poetDb, postProvider.cfg, postProvider.opts.NumUnits, opts...)
	r.Contains(err.Error(), "invalid `Challenge`")

	newNIPost := *nipost
	newNIPost.Post = &types.Post{}
	err = validateNIPost(postProvider.id, postProvider.commitmentAtxId, &newNIPost, challengeHash, poetDb, postProvider.cfg, postProvider.opts.NumUnits, opts...)
	r.Contains(err.Error(), "invalid Post")

	newPostCfg := postProvider.cfg
	newPostCfg.MinNumUnits = postProvider.opts.NumUnits + 1
	err = validateNIPost(postProvider.id, postProvider.commitmentAtxId, nipost, challengeHash, poetDb, newPostCfg, postProvider.opts.NumUnits, opts...)
	r.EqualError(err, fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", newPostCfg.MinNumUnits, postProvider.opts.NumUnits))

	newPostCfg = postProvider.cfg
	newPostCfg.MaxNumUnits = postProvider.opts.NumUnits - 1
	err = validateNIPost(postProvider.id, postProvider.commitmentAtxId, nipost, challengeHash, poetDb, newPostCfg, postProvider.opts.NumUnits, opts...)
	r.EqualError(err, fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", newPostCfg.MaxNumUnits, postProvider.opts.NumUnits))

	newPostCfg = postProvider.cfg
	newPostCfg.LabelsPerUnit = nipost.PostMetadata.LabelsPerUnit + 1
	err = validateNIPost(postProvider.id, postProvider.commitmentAtxId, nipost, challengeHash, poetDb, newPostCfg, postProvider.opts.NumUnits, opts...)
	r.EqualError(err, fmt.Sprintf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", newPostCfg.LabelsPerUnit, nipost.PostMetadata.LabelsPerUnit))
}

func validateNIPost(minerID types.NodeID, commitmentAtx types.ATXID, nipost *types.NIPost, challenge types.Hash32, poetDb poetDbAPI, postCfg PostConfig, numUnits uint32, opts ...verifying.OptionFunc) error {
	v := &Validator{poetDb, postCfg}
	_, err := v.NIPost(minerID, commitmentAtx, nipost, challenge, numUnits, opts...)
	return err
}

func TestValidateMerkleProof(t *testing.T) {
	proof := newMerkleProof(t, types.BytesToHash([]byte("challenge")), []types.Hash32{
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
		types.BytesToHash([]byte("leaf4")),
	})

	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()

		err := validateMerkleProof(proof)
		require.NoError(t, err)
	})
	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()

		invalidProof := *proof
		invalidProof.Root = types.BytesToHash([]byte("invalid root"))

		err := validateMerkleProof(&invalidProof)
		require.Error(t, err)
	})
}
