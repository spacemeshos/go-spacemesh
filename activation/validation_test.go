package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func Test_Validation_VRFNonce(t *testing.T) {
	r := require.New(t)

	// Arrange
	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	postCfg.LabelsPerUnit = 128
	meta := types.PostMetadata{
		LabelsPerUnit: postCfg.LabelsPerUnit,
	}

	initOpts := DefaultPostSetupOpts()
	initOpts.DataDir = t.TempDir()
	initOpts.ProviderID.SetUint32(initialization.CPUProviderID())

	nodeId := types.BytesToNodeID(make([]byte, 32))
	commitmentAtxId := types.EmptyATXID

	init, err := initialization.NewInitializer(
		initialization.WithNodeId(nodeId.Bytes()),
		initialization.WithCommitmentAtxId(commitmentAtxId.Bytes()),
		initialization.WithConfig(postCfg.ToConfig()),
		initialization.WithInitOpts(initOpts.ToInitOpts()),
	)
	r.NoError(err)
	r.NoError(init.Initialize(context.Background()))
	r.NotNil(init.Nonce())

	nonce := *(*types.VRFPostIndex)(init.Nonce())
	require.NotNil(t, nonce)

	v := NewValidator(nil, poetDbAPI, postCfg, initOpts.Scrypt, nil)

	// Act & Assert
	t.Run("valid vrf nonce", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits))
	})

	t.Run("invalid vrf nonce", func(t *testing.T) {
		t.Parallel()

		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, types.VRFPostIndex(1), meta, initOpts.NumUnits))
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

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, nil)

	// Act & Assert
	t.Run("valid initial nipost challenge passes", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		commitmentAtxId := types.ATXID{5, 6, 7}

		challenge := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   2,
			PositioningATX: posAtxId,
			CommitmentATX:  &commitmentAtxId,
		}
		challenge.InitialPost = &types.Post{}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(commitmentAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 1,
			},
		}, nil)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID)
		require.NoError(t, err)
	})

	t.Run("valid initial nipost challenge in epoch 1 passes", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}

		challenge := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PositioningATX: posAtxId,
			CommitmentATX:  &goldenATXID,
		}
		challenge.InitialPost = &types.Post{}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID)
		require.NoError(t, err)
	})

	t.Run("commitment atx from wrong pub layer", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		commitmentAtxId := types.ATXID{5, 6, 7}

		challenge := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   1,
			PositioningATX: posAtxId,
			CommitmentATX:  &commitmentAtxId,
		}
		challenge.InitialPost = &types.Post{}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(commitmentAtxId).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: 2,
			},
		}, nil)

		err := v.InitialNIPostChallenge(&challenge, atxProvider, goldenATXID)
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

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, nil)

	// Act & Assert
	t.Run("valid nipost challenge passes", func(t *testing.T) {
		t.Parallel()

		nodeId := types.RandomNodeID()

		prevAtxId := types.ATXID{3, 2, 1}
		posAtxId := types.ATXID{1, 2, 3}

		challenge := types.NIPostChallenge{
			Sequence:       10,
			PrevATXID:      prevAtxId,
			PublishEpoch:   2,
			PositioningATX: posAtxId,
			CommitmentATX:  nil,
		}

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

		challenge := types.NIPostChallenge{
			Sequence:       10,
			PrevATXID:      prevAtxId,
			PublishEpoch:   101,
			PositioningATX: posAtxId,
			CommitmentATX:  nil,
		}

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

		challenge := types.NIPostChallenge{
			Sequence:       10,
			PrevATXID:      prevAtxId,
			PublishEpoch:   101,
			PositioningATX: posAtxId,
			CommitmentATX:  nil,
		}

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

		challenge := types.NIPostChallenge{
			Sequence:       10,
			PrevATXID:      prevAtxId,
			PublishEpoch:   2,
			PositioningATX: posAtxId,
			CommitmentATX:  nil,
		}

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

		challenge := types.NIPostChallenge{
			Sequence:       10,
			PrevATXID:      prevAtxId,
			PublishEpoch:   2,
			PositioningATX: posAtxId,
			CommitmentATX:  nil,
		}

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
}

func Test_Validation_Post(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	postVerifier := NewMockPostVerifier(ctrl)

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, postVerifier)

	post := types.Post{}
	meta := types.PostMetadata{}

	postVerifier.EXPECT().Verify(gomock.Any(), (*shared.Proof)(&post), gomock.Any(), gomock.Any()).Return(nil)
	require.NoError(t, v.Post(context.Background(), types.EmptyNodeID, types.RandomATXID(), post, meta, 1))

	postVerifier.EXPECT().
		Verify(gomock.Any(), (*shared.Proof)(&post), gomock.Any(), gomock.Any()).
		Return(errors.New("invalid"))
	require.Error(t, v.Post(context.Background(), types.EmptyNodeID, types.RandomATXID(), post, meta, 1))
}

func Test_Validation_PositioningAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, nil)

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

		err := v.PositioningAtx(posAtxId, atxProvider, goldenAtxId, 2)
		require.NoError(t, err)
	})

	t.Run("golden ATX is allowed as positioning atx in genesis epoch", func(t *testing.T) {
		t.Parallel()

		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.PositioningAtx(goldenAtxId, atxProvider, goldenAtxId, types.LayerID(1012).GetEpoch())
		require.NoError(t, err)
	})

	t.Run("golden ATX is allowed as positioning atx in non-genesis epoch", func(t *testing.T) {
		t.Parallel()

		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.PositioningAtx(goldenAtxId, atxProvider, goldenAtxId, 5)
		require.NoError(t, err)
	})

	t.Run("fail when posAtx is not found", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(posAtxId).Return(nil, errors.New("db error"))

		err := v.PositioningAtx(posAtxId, atxProvider, goldenAtxId, types.LayerID(1012).GetEpoch())
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

		err := v.PositioningAtx(posAtxId, atxProvider, goldenAtxId, 3)
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

		err := v.PositioningAtx(posAtxId, atxProvider, goldenAtxId, 10)
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

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, nil)

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
		require.EqualError(
			t,
			err,
			fmt.Sprintf("invalid `numUnits`; expected: >=%d, given: %d", postCfg.MinNumUnits, postCfg.MinNumUnits-1),
		)

		err = v.NumUnits(&postCfg, postCfg.MaxNumUnits+1)
		require.EqualError(
			t,
			err,
			fmt.Sprintf("invalid `numUnits`; expected: <=%d, given: %d", postCfg.MaxNumUnits, postCfg.MaxNumUnits+1),
		)
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

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, nil)

	// Act & Assert
	t.Run("valid post metadata", func(t *testing.T) {
		t.Parallel()

		meta := types.PostMetadata{
			LabelsPerUnit: postCfg.LabelsPerUnit,
		}

		err := v.PostMetadata(&postCfg, meta)
		require.NoError(t, err)
	})

	t.Run("wrong labels per unit", func(t *testing.T) {
		t.Parallel()

		meta := types.PostMetadata{
			LabelsPerUnit: postCfg.LabelsPerUnit - 1,
		}

		err := v.PostMetadata(&postCfg, meta)
		require.EqualError(
			t,
			err,
			fmt.Sprintf(
				"invalid `LabelsPerUnit`; expected: >=%d, given: %d",
				postCfg.LabelsPerUnit,
				postCfg.LabelsPerUnit-1,
			),
		)
	})
}

func TestValidateMerkleProof(t *testing.T) {
	challenge := types.CalcHash32([]byte("challenge"))

	proof, root := newMerkleProof(t, challenge, []types.Hash32{
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
		types.BytesToHash([]byte("leaf4")),
	})

	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()

		err := validateMerkleProof(challenge[:], &proof, root[:])
		require.NoError(t, err)
	})
	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()

		invalidProof := proof
		invalidProof.Nodes = append([]types.Hash32{}, invalidProof.Nodes...)
		invalidProof.Nodes[0] = types.BytesToHash([]byte("invalid leaf"))

		err := validateMerkleProof(challenge[:], &invalidProof, root[:])
		require.Error(t, err)
	})
	t.Run("invalid proof - different root", func(t *testing.T) {
		t.Parallel()

		err := validateMerkleProof(challenge[:], &proof, []byte("expected root"))
		require.Error(t, err)
	})
}

func TestVerifyChainDeps(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()
	goldenATXID := types.ATXID{2, 3, 4}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	ch := types.NIPostChallenge{
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   postGenesisEpoch,
		PositioningATX: goldenATXID,
		CommitmentATX:  &goldenATXID,
	}
	nipostData := newNIPostWithChallenge(t, types.HexToHash32(""), []byte("00"))
	invalidAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(signer, invalidAtx))
	vInvalidAtx, err := invalidAtx.Verify(0, 1)
	require.NoError(t, err)
	vInvalidAtx.Validity = types.Invalid
	require.NoError(t, atxs.Add(db, vInvalidAtx))

	t.Run("invalid prev ATX", func(t *testing.T) {
		ch := types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      vInvalidAtx.ID(),
			PublishEpoch:   postGenesisEpoch,
			PositioningATX: goldenATXID,
			CommitmentATX:  nil,
		}
		nipostData = newNIPostWithChallenge(t, types.HexToHash32(""), []byte("01"))
		atx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(signer, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		vAtx.Validity = types.Unknown
		require.NoError(t, atxs.Add(db, vAtx))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		v.EXPECT().Verify(ctx, (*shared.Proof)(&atx.NIPost.Post), gomock.Any(), gomock.Any())

		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: invalidAtx.ID()})
	})

	t.Run("invalid pos ATX", func(t *testing.T) {
		ch := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   postGenesisEpoch,
			PositioningATX: vInvalidAtx.ID(),
			CommitmentATX:  nil,
		}
		nipostData = newNIPostWithChallenge(t, types.HexToHash32(""), []byte("02"))
		atx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(signer, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		vAtx.Validity = types.Unknown
		require.NoError(t, atxs.Add(db, vAtx))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		v.EXPECT().Verify(ctx, (*shared.Proof)(&atx.NIPost.Post), gomock.Any(), gomock.Any())

		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: invalidAtx.ID()})
	})

	t.Run("invalid commitment ATX", func(t *testing.T) {
		commitmentAtxID := vInvalidAtx.ID()
		ch := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   postGenesisEpoch,
			PositioningATX: goldenATXID,
			CommitmentATX:  &commitmentAtxID,
		}
		nipostData = newNIPostWithChallenge(t, types.HexToHash32(""), []byte("03"))
		atx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(signer, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		vAtx.Validity = types.Unknown
		require.NoError(t, atxs.Add(db, vAtx))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		v.EXPECT().Verify(ctx, (*shared.Proof)(&atx.NIPost.Post), gomock.Any(), gomock.Any())
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: invalidAtx.ID()})
	})

	t.Run("with trusted node ID", func(t *testing.T) {
		ch := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   postGenesisEpoch,
			PositioningATX: vInvalidAtx.ID(),
			CommitmentATX:  nil,
		}
		nipostData = newNIPostWithChallenge(t, types.HexToHash32(""), []byte("04"))
		atx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(signer, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		vAtx.Validity = types.Unknown
		require.NoError(t, atxs.Add(db, vAtx))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID, VerifyChainOpts.WithTrustedID(signer.NodeID()))
		require.NoError(t, err)
	})

	t.Run("assume valid if older than X", func(t *testing.T) {
		ch := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   postGenesisEpoch,
			PositioningATX: vInvalidAtx.ID(),
			CommitmentATX:  nil,
		}
		nipostData = newNIPostWithChallenge(t, types.HexToHash32(""), []byte("05"))
		atx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(signer, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		vAtx.Validity = types.Unknown
		require.NoError(t, atxs.Add(db, vAtx))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		before := time.Now().Add(10 * time.Second)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID, VerifyChainOpts.AssumeValidBefore(before))
		require.NoError(t, err)
	})

	t.Run("invalid top-level", func(t *testing.T) {
		ch := types.NIPostChallenge{
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   postGenesisEpoch,
			PositioningATX: vInvalidAtx.ID(),
			CommitmentATX:  nil,
		}
		nipostData = newNIPostWithChallenge(t, types.HexToHash32(""), []byte("06"))
		atx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(signer, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		vAtx.Validity = types.Unknown
		require.NoError(t, atxs.Add(db, vAtx))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		expected := errors.New("post is invalid")
		v.EXPECT().Verify(ctx, (*shared.Proof)(&atx.NIPost.Post), gomock.Any(), gomock.Any()).Return(expected)
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: vAtx.ID()})
		require.ErrorIs(t, err, expected)
	})
}

func TestIsVerifyingFullPost(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		t.Parallel()
		validator := NewValidator(nil, nil, PostConfig{K2: 7, K3: 7}, config.ScryptParams{}, nil)
		require.True(t, validator.IsVerifyingFullPost())
	})

	t.Run("partial", func(t *testing.T) {
		t.Parallel()
		validator := NewValidator(nil, nil, PostConfig{K2: 7, K3: 2}, config.ScryptParams{}, nil)
		require.False(t, validator.IsVerifyingFullPost())
	})
}
