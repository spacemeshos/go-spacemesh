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
	"github.com/spacemeshos/go-spacemesh/activation/mocks"
	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	goldenATXID = types.RandomATXID()
	atxNotFound = errors.New("unavailable")
)

func getTestConfig(t *testing.T) (atypes.PostConfig, atypes.PostSetupOpts) {
	cfg := activation.DefaultPostConfig()

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.NumUnits = cfg.MinNumUnits
	opts.ComputeProviderID = initialization.CPUProviderID()

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
	sigVerifier := signing.DefaultVerifier
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

	verifier := activation.NewChallengeVerifier(mocks.NewMockAtxProvider(gomock.NewController(t)), &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
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

	sigVerifier := signing.DefaultVerifier

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	postConfig, opts := getTestConfig(t)
	mgr, err := activation.NewPostSetupManager(nodeID, postConfig, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Generate proof.
	validPost, validPostMeta, err := mgr.GenerateProof(shared.ZeroChallenge, goldenATXID)
	req.NoError(err)

	verifier := activation.NewChallengeVerifier(mocks.NewMockAtxProvider(gomock.NewController(t)), &sigVerifier, postConfig, goldenATXID)

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)
		challengeHash, err := challenge.Hash()
		req.NoError(err)

		hash, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.NoError(err)
		req.Equal(challengeHash.Bytes(), hash)
	})
	t.Run("Sequence != 0", func(t *testing.T) {
		t.Parallel()
		challenge := createInitialChallenge(*validPost, *validPostMeta, postConfig.MinNumUnits)
		challenge.Sequence = 1
		challengeBytes, err := codec.Encode(&challenge)
		req.NoError(err)

		_, err = verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "initial challenge sequence number is not zero")
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
		req.ErrorContains(err, "initial challenge is missing commitmentATX")
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

	sigVerifier := signing.DefaultVerifier

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
		atxProvider := mocks.NewMockAtxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(gomock.Any()).AnyTimes().Return(
			&types.ActivationTxHeader{
				NIPostChallenge: types.NIPostChallenge{
					Sequence: 0,
				},
				ID:     goldenATXID,
				NodeID: nodeID,
			}, nil)
		verifier := activation.NewChallengeVerifier(atxProvider, &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
		hash, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.NoError(err)
		req.Equal(challengeHash.Bytes(), hash)
	})
	t.Run("positioning ATX unavailable", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := mocks.NewMockAtxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).AnyTimes().Return(nil, atxNotFound)
		verifier := activation.NewChallengeVerifier(atxProvider, &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrCouldntVerify)
		req.ErrorContains(err, "positioning ATX not found")
	})

	t.Run("NodeID doesn't match previous ATX NodeID", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := mocks.NewMockAtxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).Return(&types.ActivationTxHeader{}, nil)
		verifier := activation.NewChallengeVerifier(atxProvider, &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "NodeID mismatch")
	})
	t.Run("previous ATX unavailable", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := mocks.NewMockAtxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).Return(nil, atxNotFound)
		verifier := activation.NewChallengeVerifier(atxProvider, &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrCouldntVerify)
		req.ErrorContains(err, "previous ATX not found")
	})
	t.Run("publayerID is not after previousATX.publayerID ", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := mocks.NewMockAtxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).Return(&types.ActivationTxHeader{}, nil)
		atxProvider.EXPECT().GetAtxHeader(challenge.PrevATXID).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:   0,
				PubLayerID: challenge.PubLayerID,
			},
			NodeID: nodeID,
		}, nil)
		verifier := activation.NewChallengeVerifier(atxProvider, &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "challenge publication layer")
	})
	t.Run("publayerID is not after positioningATX.publayerID", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		atxProvider := mocks.NewMockAtxProvider(ctrl)
		atxProvider.EXPECT().GetAtxHeader(challenge.PositioningATX).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:   0,
				PubLayerID: challenge.PubLayerID,
			},
			NodeID: nodeID,
		}, nil)
		verifier := activation.NewChallengeVerifier(atxProvider, &sigVerifier, activation.DefaultPostConfig(), goldenATXID)
		_, err := verifier.Verify(context.Background(), challengeBytes, ed25519.Sign2(privKey, challengeBytes))
		req.ErrorIs(err, activation.ErrChallengeInvalid)
		req.ErrorContains(err, "challenge publication layer")
	})
}
