package activation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const layersPerEpoch = 10

var (
	goldenATXID    = types.RandomATXID()
	errAtxNotFound = errors.New("unavailable")
)

func getTestConfig(t *testing.T) (activation.PostConfig, activation.PostSetupOpts) {
	cfg := activation.DefaultPostConfig()

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.NumUnits = cfg.MinNumUnits
	opts.ComputeProviderID = int(initialization.CPUProviderID())

	return cfg, opts
}

func randomATXID() *types.ATXID {
	id := types.RandomATXID()
	return &id
}

func createInitialChallenge(post types.Post, meta types.PostMetadata, numUnits uint32) types.PoetChallenge {
	return types.PoetChallenge{
		NIPostChallenge: &types.NIPostChallenge{
			PositioningATX:     goldenATXID,
			CommitmentATX:      &goldenATXID,
			InitialPostIndices: post.Indices,
		},
		InitialPost:         &post,
		InitialPostMetadata: &meta,
		NumUnits:            numUnits,
	}
}

func Test_SignatureVerification(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	ctrl := gomock.NewController(t)
	atxProvider := activation.NewMockatxProvider(ctrl)
	validator := activation.NewMocknipostValidator(ctrl)

	extractor, err := signing.NewPubKeyExtractor()
	req.NoError(err)
	challenge := types.PoetChallenge{
		NIPostChallenge: &types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      *randomATXID(),
			PubLayerID:     10,
			PositioningATX: types.RandomATXID(),
		},
		NumUnits: 1,
	}
	challengeBytes, err := codec.Encode(&challenge)
	req.NoError(err)

	var sig types.EdSignature
	sig[types.EdSignatureSize-1] = 0xff

	verifier := activation.NewChallengeVerifier(atxProvider, extractor, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
	_, err = verifier.Verify(context.Background(), challengeBytes, sig)
	req.ErrorIs(err, activation.ErrSignatureInvalid)
}

// Test challenge validation for challenges carrying an initial Post challenge.
func Test_ChallengeValidation_Initial(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	sigVerifier, err := signing.NewPubKeyExtractor()
	req.NoError(err)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	postConfig, opts := getTestConfig(t)
	mgr, err := activation.NewPostSetupManager(signer.NodeID(), postConfig, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Generate proof.
	validPost, validPostMeta, err := mgr.GenerateProof(context.Background(), shared.ZeroChallenge)
	req.NoError(err)

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PostMetadata(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)
		result, err := verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.NoError(err)
		req.Equal(challenge.Hash(), result.Hash)
		req.EqualValues(signer.NodeID(), result.NodeID)
	})

	t.Run("invalid initial NIPostChallenge", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.Sequence = 1
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("invalid initial nipost challenge")).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)
		_, err = verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "invalid initial nipost challenge")
	})

	t.Run("InitialPost not provided", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.InitialPost = nil
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)
		_, err = verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "initial Post is not included")
	})

	t.Run("invalid post proof", func(t *testing.T) {
		t.Parallel()
		invalidPost := *validPost
		invalidPost.Nonce += 1
		challenge := createInitialChallenge(invalidPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.InitialPost.Nonce += 1
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PostMetadata(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("invalid post")).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)
		_, err = verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "invalid post")
	})

	t.Run("invalid num units", func(t *testing.T) {
		t.Parallel()
		invalidPost := *validPost
		invalidPost.Nonce += 1
		challenge := createInitialChallenge(invalidPost, *validPostMeta, postConfig.MinNumUnits-1)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(errors.New("wrong num units")).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)
		_, err = verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "wrong num units")
	})

	t.Run("invalid post metadata", func(t *testing.T) {
		t.Parallel()
		invalidPostMeta := *validPostMeta
		invalidPostMeta.LabelsPerUnit = 0
		challenge := createInitialChallenge(*validPost, invalidPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PostMetadata(gomock.Any(), gomock.Any()).Return(errors.New("wrong post metadata")).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)
		_, err = verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "wrong post metadata")
	})
}

// Test challenge validation for subsequent challenges that
// use previous ATX as the part of the challenge.
func Test_ChallengeValidation_NonInitial(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	sigVerifier, err := signing.NewPubKeyExtractor()
	req.NoError(err)

	challenge := types.PoetChallenge{
		NIPostChallenge: &types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      *randomATXID(),
			PubLayerID:     10,
			PositioningATX: types.RandomATXID(),
		},
		NumUnits: 1,
	}
	challengeBytes, err := codec.Encode(&challenge)
	req.NoError(err)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(gomock.Any()).AnyTimes().Return(
			&types.ActivationTxHeader{
				NIPostChallenge: types.NIPostChallenge{
					Sequence: 0,
				},
				ID:     goldenATXID,
				NodeID: signer.NodeID(),
			}, nil)
		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		result, err := verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.NoError(err)
		req.Equal(challenge.Hash(), result.Hash)
		req.EqualValues(signer.NodeID(), result.NodeID)
	})

	t.Run("positioning ATX validation fails with ErrAtxNotFound", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(nil, errAtxNotFound)

		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&activation.ErrAtxNotFound{Id: challenge.PositioningATX}).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, &activation.VerifyError{})
		req.ErrorIs(err, &activation.ErrAtxNotFound{Id: challenge.PositioningATX})
	})

	t.Run("positioning ATX validation fails with other error", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(&types.ActivationTxHeader{}, nil)

		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("failed positioning ATX validation")).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "failed positioning ATX validation")
	})

	t.Run("nipost challenge validation fail", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).AnyTimes().Return(&types.ActivationTxHeader{}, nil)

		validator := activation.NewMocknipostValidator(ctrl)
		validator.EXPECT().NumUnits(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		validator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("failed nipost challenge validation")).Times(1)

		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, signer.Sign(signing.POET, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "failed nipost challenge validation")
	})
}
