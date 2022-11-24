package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var (
	ErrSignatureInvalid = errors.New("signature is invalid")
	ErrChallengeInvalid = errors.New("challenge is invalid")
	ErrCouldntVerify    = errors.New("couldn't verify challenge")
)

//go:generate mockgen -package=mocks -destination=./mocks/challenge_verifier.go . AtxProvider

type ChallengeVerificationResult struct {
	Hash   types.Hash32
	NodeID types.NodeID
}

type ChallengeVerifier interface {
	Verify(ctx context.Context, challenge, signature []byte) (*ChallengeVerificationResult, error)
}

type AtxProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
}

type challengeVerifier struct {
	atxDB             AtxProvider
	signatureVerifier signing.VerifyExtractor
	cfg               atypes.PostConfig
	goldenATXID       types.ATXID
}

func NewChallengeVerifier(cdb AtxProvider, signatureVerifier signing.VerifyExtractor, cfg atypes.PostConfig, goldenATX types.ATXID) ChallengeVerifier {
	return &challengeVerifier{
		atxDB:             cdb,
		signatureVerifier: signatureVerifier,
		cfg:               cfg,
		goldenATXID:       goldenATX,
	}
}

func (v *challengeVerifier) Verify(ctx context.Context, challengeBytes, signature []byte) (*ChallengeVerificationResult, error) {
	pubkey, err := v.signatureVerifier.Extract(challengeBytes, signature)
	nodeID := types.BytesToNodeID(pubkey.Bytes())
	if err != nil {
		return nil, ErrSignatureInvalid
	}

	challenge := types.PoetChallenge{}
	if err := codec.Decode(challengeBytes, &challenge); err != nil {
		return nil, err
	}

	if err := v.verifyChallenge(ctx, &challenge, nodeID); err != nil {
		return nil, err
	}

	hash, err := challenge.Hash()
	if err != nil {
		return nil, err
	}
	return &ChallengeVerificationResult{
		Hash:   *hash,
		NodeID: nodeID,
	}, nil
}

func (v *challengeVerifier) verifyChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	log.With().Info("verifying challenge", log.Object("challenge", challenge))

	if err := validateNumUnits(&v.cfg, challenge.NumUnits); err != nil {
		return fmt.Errorf("%w: %v", ErrChallengeInvalid, err)
	}

	// Verify against positioning ATX
	if challenge.PositioningATX == v.goldenATXID && !challenge.PubLayerID.GetEpoch().IsGenesis() {
		return fmt.Errorf("%w: golden atx used for positioning atx in epoch %d, but is only valid in epoch 1", ErrChallengeInvalid, challenge.PubLayerID.GetEpoch())
	}

	if challenge.PositioningATX != v.goldenATXID {
		atx, err := v.atxDB.GetAtxHeader(challenge.PositioningATX)
		if err != nil {
			return fmt.Errorf("%w: positioning ATX not found (%v)", ErrCouldntVerify, err)
		}
		if !challenge.PubLayerID.After(atx.PubLayerID) {
			return fmt.Errorf("%w: challenge publication layer (%v) must be after positioning atx layer (%v)", ErrChallengeInvalid, challenge.PubLayerID, atx.PubLayerID)
		}
	}

	if challenge.PrevATXID == *types.EmptyATXID {
		return v.verifyInitialChallenge(ctx, challenge, nodeID)
	} else {
		return v.verifyNonInitialChallenge(ctx, challenge, nodeID)
	}
}

func (v *challengeVerifier) verifyInitialChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	if challenge.Sequence != 0 {
		return fmt.Errorf("%w: initial challenge sequence number is not zero", ErrChallengeInvalid)
	}

	if challenge.InitialPost == nil {
		return fmt.Errorf("%w: initial Post is not included", ErrChallengeInvalid)
	}

	if challenge.InitialPostIndices == nil {
		return fmt.Errorf("%w: initial Post indices is not included", ErrChallengeInvalid)
	}

	if !bytes.Equal(challenge.InitialPost.Indices, challenge.InitialPostIndices) {
		return fmt.Errorf("%w: initial Post indices included in challenge don't match", ErrChallengeInvalid)
	}

	if challenge.CommitmentATX == nil {
		return fmt.Errorf("%w: initial challenge is missing commitmentATX", ErrChallengeInvalid)
	}

	if *challenge.CommitmentATX != v.goldenATXID {
		commitmentAtx, err := v.atxDB.GetAtxHeader(*challenge.CommitmentATX)
		if err != nil {
			return fmt.Errorf("%w: commitment ATX not found", ErrCouldntVerify)
		}
		if !challenge.PubLayerID.After(commitmentAtx.PubLayerID) {
			return fmt.Errorf("%w: atx layer (%v) must be after commitment atx layer (%v)", ErrChallengeInvalid, challenge.PubLayerID, commitmentAtx.PubLayerID)
		}
	}

	if err := validatePostMetadata(&v.cfg, challenge.InitialPostMetadata); err != nil {
		return fmt.Errorf("%w: invalid initial Post Metadata: %v", ErrChallengeInvalid, err)
	}

	commitment := GetCommitmentBytes(nodeID, *challenge.CommitmentATX)
	if err := validatePost(commitment, challenge.InitialPost, challenge.InitialPostMetadata, challenge.NumUnits); err != nil {
		return fmt.Errorf("%w: invalid initial Post: %v", ErrChallengeInvalid, err)
	}

	return nil
}

func (v *challengeVerifier) verifyNonInitialChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	// Verify against previous ATX
	atx, err := v.atxDB.GetAtxHeader(challenge.PrevATXID)
	if err != nil {
		return fmt.Errorf("%w previous ATX not found (%v)", ErrCouldntVerify, err)
	}
	if !challenge.PubLayerID.After(atx.PubLayerID) {
		return fmt.Errorf("%w: challenge publication layer (%v) must be after previous atx layer (%v)", ErrChallengeInvalid, challenge.PubLayerID, atx.PubLayerID)
	}
	if atx.NodeID != nodeID {
		return fmt.Errorf("%w: NodeID mismatch: challenge: %X, previous ATX: %X", ErrChallengeInvalid, nodeID, atx.NodeID)
	}

	if challenge.Sequence != atx.Sequence+1 {
		return fmt.Errorf("%w: sequence number is not one more than prev sequence number", ErrChallengeInvalid)
	}

	if challenge.InitialPost != nil {
		return fmt.Errorf("%w: prevATX declared, but initial Post is included", ErrChallengeInvalid)
	}

	if challenge.InitialPostIndices != nil {
		return fmt.Errorf("%w: prevATX declared, but initial Post indices is included in challenge", ErrChallengeInvalid)
	}

	if challenge.CommitmentATX != nil {
		return fmt.Errorf("%w: prevATX declared, but commitmentATX is included", ErrChallengeInvalid)
	}

	return nil
}
