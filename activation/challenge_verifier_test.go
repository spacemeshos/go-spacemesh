package activation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/ed25519"
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

	sigVerifier := signing.NewPubKeyExtractor()
	ctrl := gomock.NewController(t)
	atxProvider := activation.NewMockatxProvider(ctrl)
	validator := activation.NewMocknipostValidator(ctrl)

	challenge := types.PoetChallenge{
		NIPostChallenge: &types.NIPostChallenge{
			Sequence:  1,
			PrevATXID: *randomATXID(),
			PubLayerID: types.LayerID{
				Value: 10,
			},
			PositioningATX: types.RandomATXID(),
		},
		NumUnits: 1,
	}
	challengeBytes, err := codec.Encode(&challenge)
	req.NoError(err)

	verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
	_, err = verifier.Verify(context.Background(), challengeBytes, types.RandomBytes(32))
	req.ErrorIs(err, activation.ErrSignatureInvalid)
}

// Test challenge validation for challenges carrying an initial Post challenge.
func Test_ChallengeValidation_Initial(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	req.NoError(err)
	nodeID := types.BytesToNodeID(pubKey)

	sigVerifier := signing.NewPubKeyExtractor()

	ctrl := gomock.NewController(t)
	atxProvider := activation.NewMockatxProvider(ctrl)
	validator := activation.NewMocknipostValidator(ctrl)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	postConfig, opts := getTestConfig(t)
	mgr, err := activation.NewPostSetupManager(nodeID, postConfig, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID))

	// Generate proof.
	validPost, validPostMeta, err := mgr.GenerateProof(shared.ZeroChallenge)
	req.NoError(err)

	verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, postConfig, goldenATXID, layersPerEpoch)

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)
		challengeHash, err := challenge.Hash()
		req.NoError(err)

		result, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.NoError(err)
		req.Equal(*challengeHash, result.Hash)
		req.EqualValues(pubKey, result.NodeID.Bytes())
	})
	t.Run("Sequence != 0", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.Sequence = 1
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "no prevATX declared, but sequence number not zero")
	})
	t.Run("InitialPost not provided", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.InitialPost = nil
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "initial Post is not included")
	})
	t.Run("CommitmentATX not provided", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.CommitmentATX = nil
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "no prevATX declared, but commitmentATX is missing")
	})
	t.Run("InitialPostIndices not provided", func(t *testing.T) {
		t.Parallel()

		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.InitialPostIndices = nil
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "initial Post indices is not included")
	})
	t.Run("invalid post proof", func(t *testing.T) {
		t.Parallel()
		invalidPost := *validPost
		invalidPost.Nonce += 1
		challenge := createInitialChallenge(invalidPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.InitialPost.Nonce += 1
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
	})
	t.Run("invalid post metadata (NumUnits < MinNumUnits)", func(t *testing.T) {
		t.Parallel()
		invalidPost := *validPost
		invalidPost.Nonce += 1
		challenge := createInitialChallenge(invalidPost, *validPostMeta, postConfig.MinNumUnits-1)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
	})
	t.Run("invalid post metadata (NumUnits > MaxNumUnits)", func(t *testing.T) {
		t.Parallel()
		invalidPost := *validPost
		invalidPost.Nonce += 1
		challenge := createInitialChallenge(invalidPost, *validPostMeta, postConfig.MaxNumUnits+1)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
	})
	t.Run("invalid post metadata (BitsPerLabel)", func(t *testing.T) {
		t.Parallel()
		invalidPostMeta := *validPostMeta
		invalidPostMeta.BitsPerLabel += 1
		challenge := createInitialChallenge(*validPost, invalidPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
	})
	t.Run("invalid post metadata (LabelsPerUnit)", func(t *testing.T) {
		t.Parallel()
		invalidPostMeta := *validPostMeta
		invalidPostMeta.LabelsPerUnit = 0
		challenge := createInitialChallenge(*validPost, invalidPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
	})
	t.Run("invalid post metadata (K1)", func(t *testing.T) {
		t.Parallel()
		invalidPostMeta := *validPostMeta
		invalidPostMeta.K1 += 1
		challenge := createInitialChallenge(*validPost, invalidPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
	})
	t.Run("invalid post metadata (K2)", func(t *testing.T) {
		t.Parallel()
		invalidPostMeta := *validPostMeta
		invalidPostMeta.K2 -= 1
		challenge := createInitialChallenge(*validPost, invalidPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "K2")
	})
}

// Test challenge validation for subsequent challenges that
// use previous ATX as the part of the challenge.
func Test_ChallengeValidation_NonInitial(t *testing.T) {
	t.Parallel()
	req := require.New(t)

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	req.NoError(err)
	nodeID := types.BytesToNodeID(pubKey)

	sigVerifier := signing.NewPubKeyExtractor()

	challenge := types.PoetChallenge{
		NIPostChallenge: &types.NIPostChallenge{
			Sequence:  1,
			PrevATXID: *randomATXID(),
			PubLayerID: types.LayerID{
				Value: 10,
			},
			PositioningATX: types.RandomATXID(),
		},
		NumUnits: 1,
	}
	challengeBytes, err := codec.Encode(&challenge)
	req.NoError(err)
	challengeHash, err := challenge.Hash()
	req.NoError(err)

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
				NodeID: nodeID,
			}, nil)
		validator := activation.NewMocknipostValidator(ctrl)
		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		result, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.NoError(err)
		req.Equal(*challengeHash, result.Hash)
		req.EqualValues(pubKey, result.NodeID.Bytes())
	})
	t.Run("positioning ATX unavailable", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(nil, errAtxNotFound)
		validator := activation.NewMocknipostValidator(ctrl)
		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, &activation.VerifyError{})
		req.ErrorIs(err, &activation.ErrAtxNotFound{Id: challenge.PositioningATX})
	})

	t.Run("NodeID doesn't match previous ATX NodeID", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).AnyTimes().Return(&types.ActivationTxHeader{}, nil)
		validator := activation.NewMocknipostValidator(ctrl)
		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "previous atx belongs to different miner")
	})
	t.Run("previous ATX unavailable", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).AnyTimes().Return(nil, errAtxNotFound)
		validator := activation.NewMocknipostValidator(ctrl)
		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, &activation.VerifyError{})
		req.ErrorIs(err, &activation.ErrAtxNotFound{Id: challenge.PrevATXID})
	})
	t.Run("publayerID is not after previousATX.publayerID ", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).AnyTimes().Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:   0,
				PubLayerID: challenge.PubLayerID,
			},
			NodeID: nodeID,
		}, nil)
		validator := activation.NewMocknipostValidator(ctrl)
		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "prevAtx epoch (1, layer 10) isn't older than current atx epoch (1, layer 10)")
	})
	t.Run("publayerID is not after positioningATX.publayerID", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := activation.NewMockatxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:   0,
				PubLayerID: challenge.PubLayerID,
			},
			NodeID: nodeID,
		}, nil)
		validator := activation.NewMocknipostValidator(ctrl)
		verifier := activation.NewChallengeVerifier(atxProvider, sigVerifier, validator, activation.DefaultPostConfig(), goldenATXID, layersPerEpoch)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "positioning atx layer (10) must be before 10")
	})
}
