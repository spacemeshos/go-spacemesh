package activation

import (
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
)

type VerifyError struct {
	// the source (if any) that caused the error
	source error
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("couldn't verify challenge (%v)", e.source)
}

func (e *VerifyError) Unwrap() error { return e.source }

func (e *VerifyError) Is(target error) bool {
	_, ok := target.(*VerifyError)
	return ok
}

//go:generate mockgen -package=activation -destination=./challenge_verifier_mocks.go . AtxProvider

type ChallengeVerificationResult struct {
	Hash   types.Hash32
	NodeID types.NodeID
}

type ChallengeVerifier interface {
	Verify(ctx context.Context, challenge, signature []byte) (*ChallengeVerificationResult, error)
}

type challengeVerifier struct {
	atxDB             AtxProvider
	signatureVerifier signing.VerifyExtractor
	cfg               atypes.PostConfig
	goldenATXID       types.ATXID
	layersPerEpoch    uint32
}

func NewChallengeVerifier(cdb AtxProvider, signatureVerifier signing.VerifyExtractor, cfg atypes.PostConfig, goldenATX types.ATXID, layersPerEpoch uint32) ChallengeVerifier {
	return &challengeVerifier{
		atxDB:             cdb,
		signatureVerifier: signatureVerifier,
		cfg:               cfg,
		goldenATXID:       goldenATX,
		layersPerEpoch:    layersPerEpoch,
	}
}

func (v *challengeVerifier) Verify(ctx context.Context, challengeBytes, signature []byte) (*ChallengeVerificationResult, error) {
	pubkey, err := v.signatureVerifier.Extract(challengeBytes, signature)
	if err != nil {
		return nil, ErrSignatureInvalid
	}
	nodeID := types.BytesToNodeID(pubkey.Bytes())

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

	if err := validatePositioningAtx(&challenge.PositioningATX, v.atxDB, v.goldenATXID, challenge.PubLayerID, v.layersPerEpoch); err != nil {
		switch err.(type) {
		case *AtxNotFoundError:
			return &VerifyError{source: err}
		default:
			return fmt.Errorf("%w: %v", ErrChallengeInvalid, err)
		}
	}

	if challenge.PrevATXID == *types.EmptyATXID {
		return v.verifyInitialChallenge(ctx, challenge, nodeID)
	} else {
		return v.verifyNonInitialChallenge(ctx, challenge, nodeID)
	}
}

func (v *challengeVerifier) verifyInitialChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	if challenge.InitialPost == nil {
		return fmt.Errorf("%w: initial Post is not included", ErrChallengeInvalid)
	}

	if err := validateInitialNIPostChallenge(challenge.NIPostChallenge, v.atxDB, v.goldenATXID, challenge.InitialPost.Indices); err != nil {
		switch err.(type) {
		case *AtxNotFoundError:
			return &VerifyError{source: err}
		default:
			return fmt.Errorf("%w: %v", ErrChallengeInvalid, err)
		}
	}

	if err := validatePostMetadata(&v.cfg, challenge.InitialPostMetadata); err != nil {
		return fmt.Errorf("%w: invalid initial Post Metadata: %v", ErrChallengeInvalid, err)
	}

	if err := validatePost(nodeID, *challenge.CommitmentATX, challenge.InitialPost, challenge.InitialPostMetadata, challenge.NumUnits); err != nil {
		return fmt.Errorf("%w: invalid initial Post: %v", ErrChallengeInvalid, err)
	}

	return nil
}

func (v *challengeVerifier) verifyNonInitialChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	if err := validateNonInitialNIPostChallenge(challenge.NIPostChallenge, v.atxDB, nodeID); err != nil {
		switch err.(type) {
		case *AtxNotFoundError:
			return &VerifyError{source: err}
		default:
			return fmt.Errorf("%w: %v", ErrChallengeInvalid, err)
		}
	}

	if challenge.InitialPost != nil {
		return fmt.Errorf("%w: prevATX declared, but initial Post is included", ErrChallengeInvalid)
	}

	return nil
}
