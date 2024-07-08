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

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_Validation_VRFNonce(t *testing.T) {
	r := require.New(t)

	// Arrange
	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	postCfg.LabelsPerUnit = 128
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

	nonce := *init.Nonce()

	v := NewValidator(nil, poetDbAPI, postCfg, initOpts.Scrypt, nil)

	// Act & Assert
	t.Run("valid vrf nonce", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, postCfg.LabelsPerUnit, initOpts.NumUnits))
	})

	t.Run("invalid vrf nonce", func(t *testing.T) {
		t.Parallel()

		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, 1, postCfg.LabelsPerUnit, initOpts.NumUnits))
	})

	t.Run("wrong commitmentAtxId", func(t *testing.T) {
		t.Parallel()

		commitmentAtxId := types.ATXID{1, 2, 3}
		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, postCfg.LabelsPerUnit, initOpts.NumUnits))
	})

	t.Run("numUnits can be smaller", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, postCfg.LabelsPerUnit, initOpts.NumUnits-1))
	})

	t.Run("invalid labels per unit", func(t *testing.T) {
		t.Parallel()

		require.Error(t, v.VRFNonce(nodeId, commitmentAtxId, nonce, postCfg.LabelsPerUnit/2, initOpts.NumUnits))
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

		challenge := wire.NIPostChallengeV1{
			Sequence:         0,
			PrevATXID:        types.EmptyATXID,
			PublishEpoch:     2,
			PositioningATXID: posAtxId,
			CommitmentATXID:  &commitmentAtxId,
			InitialPost:      &wire.PostV1{},
		}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtx(commitmentAtxId).Return(&types.ActivationTx{PublishEpoch: 1}, nil)

		err := v.InitialNIPostChallengeV1(&challenge, atxProvider, goldenATXID)
		require.NoError(t, err)
	})

	t.Run("valid initial nipost challenge in epoch 1 passes", func(t *testing.T) {
		t.Parallel()

		challenge := wire.NIPostChallengeV1{
			Sequence:         0,
			PrevATXID:        types.EmptyATXID,
			PublishEpoch:     0,
			PositioningATXID: types.RandomATXID(),
			CommitmentATXID:  &goldenATXID,
			InitialPost:      &wire.PostV1{},
		}

		atxProvider := NewMockatxProvider(ctrl)

		err := v.InitialNIPostChallengeV1(&challenge, atxProvider, goldenATXID)
		require.NoError(t, err)
	})

	t.Run("commitment atx from wrong pub layer", func(t *testing.T) {
		t.Parallel()

		commitmentAtxId := types.ATXID{5, 6, 7}
		challenge := wire.NIPostChallengeV1{
			Sequence:         0,
			PrevATXID:        types.EmptyATXID,
			PublishEpoch:     1,
			PositioningATXID: types.ATXID{1, 2, 3},
			CommitmentATXID:  &commitmentAtxId,
			InitialPost:      &wire.PostV1{},
		}
		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtx(commitmentAtxId).Return(&types.ActivationTx{PublishEpoch: 2}, nil)

		err := v.InitialNIPostChallengeV1(&challenge, atxProvider, goldenATXID)
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

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	goldenAtxID := types.RandomATXID()

	v := NewValidator(nil, poetDbAPI, postCfg, config.ScryptParams{}, nil)

	// Act & Assert
	t.Run("valid nipost challenge passes", func(t *testing.T) {
		t.Parallel()

		prevAtx := newInitialATXv1(t, goldenAtxID)
		prevAtx.Sign(sig)

		challenge := wire.NIPostChallengeV1{
			Sequence:         prevAtx.Sequence + 1,
			PrevATXID:        prevAtx.ID(),
			PublishEpoch:     prevAtx.PublishEpoch + 1,
			PositioningATXID: goldenAtxID,
		}

		err := v.NIPostChallengeV1(&challenge, wire.ActivationTxFromWireV1(prevAtx), sig.NodeID())
		require.NoError(t, err)
	})

	t.Run("prev ATX from different miner", func(t *testing.T) {
		t.Parallel()

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)
		prevAtx := newInitialATXv1(t, goldenAtxID)
		prevAtx.Sign(otherSig)

		challenge := wire.NIPostChallengeV1{
			Sequence:         prevAtx.Sequence + 1,
			PrevATXID:        prevAtx.ID(),
			PublishEpoch:     prevAtx.PublishEpoch + 1,
			PositioningATXID: goldenAtxID,
		}

		err = v.NIPostChallengeV1(&challenge, wire.ActivationTxFromWireV1(prevAtx), sig.NodeID())
		require.ErrorContains(t, err, "previous atx belongs to different miner")
	})

	t.Run("prev atx from wrong publication epoch", func(t *testing.T) {
		t.Parallel()

		prevAtx := newInitialATXv1(t, goldenAtxID)
		prevAtx.Sign(sig)

		challenge := wire.NIPostChallengeV1{
			Sequence:         prevAtx.Sequence + 1,
			PrevATXID:        prevAtx.ID(),
			PublishEpoch:     prevAtx.PublishEpoch,
			PositioningATXID: goldenAtxID,
		}

		err := v.NIPostChallengeV1(&challenge, wire.ActivationTxFromWireV1(prevAtx), sig.NodeID())
		require.EqualError(t, err, "prevAtx epoch (2) isn't older than current atx epoch (2)")
	})

	t.Run("prev atx sequence not lower than current", func(t *testing.T) {
		t.Parallel()

		prevAtx := newInitialATXv1(t, goldenAtxID, func(atx *wire.ActivationTxV1) {
			atx.Sequence = 10
		})
		prevAtx.Sign(sig)

		challenge := wire.NIPostChallengeV1{
			Sequence:         prevAtx.Sequence,
			PrevATXID:        prevAtx.ID(),
			PublishEpoch:     prevAtx.PublishEpoch + 1,
			PositioningATXID: goldenAtxID,
		}

		err := v.NIPostChallengeV1(&challenge, wire.ActivationTxFromWireV1(prevAtx), sig.NodeID())
		require.EqualError(t, err, "sequence number (10) is not one more than the prev one (10)")
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
	meta := types.PostMetadata{LabelsPerUnit: postCfg.LabelsPerUnit}

	postVerifier.EXPECT().Verify(gomock.Any(), (*shared.Proof)(&post), gomock.Any(), gomock.Any()).Return(nil)
	err := v.Post(context.Background(), types.EmptyNodeID, types.RandomATXID(), &post, &meta, 1)
	require.NoError(t, err)

	postVerifier.EXPECT().
		Verify(gomock.Any(), (*shared.Proof)(&post), gomock.Any(), gomock.Any()).
		Return(errors.New("invalid"))
	err = v.Post(context.Background(), types.EmptyNodeID, types.RandomATXID(), &post, &meta, 1)
	require.Error(t, err)
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
		atxProvider.EXPECT().GetAtx(posAtxId).Return(&types.ActivationTx{PublishEpoch: 1}, nil)

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
		atxProvider.EXPECT().GetAtx(posAtxId).Return(nil, errors.New("db error"))

		err := v.PositioningAtx(posAtxId, atxProvider, goldenAtxId, types.LayerID(1012).GetEpoch())
		require.ErrorIs(t, err, &ErrAtxNotFound{Id: posAtxId})
		require.ErrorContains(t, err, "db error")
	})

	t.Run("positioning atx published in higher epoch than expected", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtx(posAtxId).Return(&types.ActivationTx{PublishEpoch: 5}, nil)

		err := v.PositioningAtx(posAtxId, atxProvider, goldenAtxId, 3)
		require.EqualError(t, err, "positioning atx epoch (5) must be before 3")
	})

	t.Run("any distance to positioning atx is valid", func(t *testing.T) {
		t.Parallel()

		posAtxId := types.ATXID{1, 2, 3}
		goldenAtxId := types.ATXID{9, 9, 9}

		atxProvider := NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtx(posAtxId).Return(&types.ActivationTx{PublishEpoch: 1}, nil)

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

		err := v.LabelsPerUnit(&postCfg, postCfg.LabelsPerUnit)
		require.NoError(t, err)
	})

	t.Run("wrong labels per unit", func(t *testing.T) {
		t.Parallel()

		err := v.LabelsPerUnit(&postCfg, postCfg.LabelsPerUnit-1)
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

	proof, root := newMerkleProof(t, []types.Hash32{
		challenge,
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
	db := statesql.InMemory()
	ctx := context.Background()
	goldenATXID := types.ATXID{2, 3, 4}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	invalidAtx := newInitialATXv1(t, goldenATXID)
	invalidAtx.Sign(signer)
	vInvalidAtx := toAtx(t, invalidAtx)
	vInvalidAtx.SetValidity(types.Invalid)
	require.NoError(t, atxs.Add(db, vInvalidAtx, invalidAtx.Blob()))

	t.Run("invalid prev ATX", func(t *testing.T) {
		atx := newChainedActivationTxV1(t, invalidAtx, goldenATXID)
		atx.Sign(signer)
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		v.EXPECT().Verify(ctx, gomock.Any(), gomock.Any(), gomock.Any())

		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: invalidAtx.ID()})
	})

	t.Run("invalid pos ATX", func(t *testing.T) {
		atx := newInitialATXv1(t, invalidAtx.ID())
		atx.Sign(signer)
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		v.EXPECT().Verify(ctx, (*shared.Proof)(atx.NIPost.Post), gomock.Any(), gomock.Any())

		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: invalidAtx.ID()})
	})

	t.Run("invalid commitment ATX", func(t *testing.T) {
		commitmentAtxID := vInvalidAtx.ID()
		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(signer)
		atx.CommitmentATXID = &commitmentAtxID
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		v.EXPECT().Verify(ctx, (*shared.Proof)(atx.NIPost.Post), gomock.Any(), gomock.Any())
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: invalidAtx.ID()})
	})

	t.Run("with trusted node ID", func(t *testing.T) {
		atx := newInitialATXv1(t, invalidAtx.ID())
		atx.Sign(signer)
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID, VerifyChainOpts.WithTrustedID(signer.NodeID()))
		require.NoError(t, err)
	})

	t.Run("assume valid if older than X", func(t *testing.T) {
		atx := newInitialATXv1(t, invalidAtx.ID())
		atx.Sign(signer)
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		before := time.Now().Add(10 * time.Second)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID, VerifyChainOpts.AssumeValidBefore(before))
		require.NoError(t, err)
	})

	t.Run("invalid top-level", func(t *testing.T) {
		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(signer)
		vAtx := toAtx(t, atx)
		require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

		ctrl := gomock.NewController(t)
		v := NewMockPostVerifier(ctrl)
		expected := errors.New("post is invalid")
		v.EXPECT().Verify(ctx, (*shared.Proof)(atx.NIPost.Post), gomock.Any(), gomock.Any()).Return(expected)
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
		require.ErrorIs(t, err, &InvalidChainError{ID: vAtx.ID()})
		require.ErrorIs(t, err, expected)
	})

	t.Run("initial V2 ATX", func(t *testing.T) {
		watx := newInitialATXv2(t, goldenATXID)
		watx.Sign(signer)
		atx := &types.ActivationTx{
			PublishEpoch: watx.PublishEpoch,
			SmesherID:    watx.SmesherID,
		}
		atx.SetID(watx.ID())
		require.NoError(t, atxs.Add(db, atx, watx.Blob()))

		v := NewMockPostVerifier(gomock.NewController(t))
		expectedPost := (*shared.Proof)(wire.PostFromWireV1(&watx.NiPosts[0].Posts[0].Post))
		v.EXPECT().Verify(ctx, expectedPost, gomock.Any(), gomock.Any())
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, watx.ID(), goldenATXID)
		require.NoError(t, err)
	})
	t.Run("non-initial V2 ATX", func(t *testing.T) {
		initialAtx := newInitialATXv1(t, goldenATXID)
		initialAtx.Sign(signer)
		require.NoError(t, atxs.Add(db, toAtx(t, initialAtx), initialAtx.Blob()))

		watx := newSoloATXv2(t, initialAtx.PublishEpoch+1, initialAtx.ID(), initialAtx.ID())
		watx.Sign(signer)
		atx := &types.ActivationTx{
			PublishEpoch: watx.PublishEpoch,
			SmesherID:    watx.SmesherID,
		}
		atx.SetID(watx.ID())
		require.NoError(t, atxs.Add(db, atx, watx.Blob()))

		v := NewMockPostVerifier(gomock.NewController(t))
		expectedPost := (*shared.Proof)(wire.PostFromWireV1(&watx.NiPosts[0].Posts[0].Post))
		v.EXPECT().Verify(ctx, (*shared.Proof)(initialAtx.NIPost.Post), gomock.Any(), gomock.Any())
		v.EXPECT().Verify(ctx, expectedPost, gomock.Any(), gomock.Any())
		validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
		err = validator.VerifyChain(ctx, watx.ID(), goldenATXID)
		require.NoError(t, err)
	})
}

func TestVerifyChainDepsAfterCheckpoint(t *testing.T) {
	db := statesql.InMemory()
	ctx := context.Background()
	goldenATXID := types.ATXID{2, 3, 4}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	// The previous and positioning ATXs of the verified ATX will be a checkpointed (golden) ATX.
	checkpointedAtx := newInitialATXv1(t, goldenATXID)
	checkpointedAtx.Sign(signer)
	vCheckpointedAtx := toAtx(t, checkpointedAtx)
	require.NoError(t, atxs.AddCheckpointed(db, &atxs.CheckpointAtx{
		ID:             vCheckpointedAtx.ID(),
		Epoch:          vCheckpointedAtx.PublishEpoch,
		CommitmentATX:  *vCheckpointedAtx.CommitmentATX,
		VRFNonce:       vCheckpointedAtx.VRFNonce,
		NumUnits:       vCheckpointedAtx.NumUnits,
		BaseTickHeight: vCheckpointedAtx.BaseTickHeight,
		TickCount:      vCheckpointedAtx.TickCount,
		SmesherID:      vCheckpointedAtx.SmesherID,
		Sequence:       vCheckpointedAtx.Sequence,
		Coinbase:       vCheckpointedAtx.Coinbase,
	}))

	atx := newChainedActivationTxV1(t, checkpointedAtx, checkpointedAtx.ID())
	atx.Sign(signer)
	vAtx := toAtx(t, atx)
	require.NoError(t, atxs.Add(db, vAtx, atx.Blob()))

	ctrl := gomock.NewController(t)
	v := NewMockPostVerifier(ctrl)
	v.EXPECT().Verify(ctx, gomock.Any(), gomock.Any(), gomock.Any())

	validator := NewValidator(db, nil, DefaultPostConfig(), config.ScryptParams{}, v)
	err = validator.VerifyChain(ctx, vAtx.ID(), goldenATXID)
	require.NoError(t, err)
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
