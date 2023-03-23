package activation

import (
	"context"
	"errors"
	"fmt"

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

type ChallengeVerificationResult struct {
	Hash   types.Hash32
	NodeID types.NodeID
}

type ChallengeVerifier interface {
	Verify(ctx context.Context, challenge []byte, signature [64]byte) (*ChallengeVerificationResult, error)
}

type challengeVerifier struct {
	atxDB          atxProvider
	keyExtractor   *signing.PubKeyExtractor
	validator      nipostValidator
	cfg            PostConfig
	goldenATXID    types.ATXID
	layersPerEpoch uint32
}

func NewChallengeVerifier(cdb atxProvider, signatureVerifier *signing.PubKeyExtractor, validator nipostValidator, cfg PostConfig, goldenATX types.ATXID, layersPerEpoch uint32) *challengeVerifier {
	return &challengeVerifier{
		atxDB:          cdb,
		keyExtractor:   signatureVerifier,
		validator:      validator,
		cfg:            cfg,
		goldenATXID:    goldenATX,
		layersPerEpoch: layersPerEpoch,
	}
}

func (v *challengeVerifier) Verify(ctx context.Context, challengeBytes []byte, signature [64]byte) (*ChallengeVerificationResult, error) {
	nodeID, err := v.keyExtractor.ExtractNodeID(signing.POET, challengeBytes, signature)
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

	return &ChallengeVerificationResult{
		Hash:   challenge.Hash(),
		NodeID: nodeID,
	}, nil
}

func (v *challengeVerifier) verifyChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	log.GetLogger().WithContext(ctx).With().Info("Verifying challenge", log.Object("challenge", challenge))

	if err := v.validator.NumUnits(&v.cfg, challenge.NumUnits); err != nil {
		return fmt.Errorf("%w: %v", ErrChallengeInvalid, err)
	}

	if err := v.validator.PositioningAtx(&challenge.PositioningATX, v.atxDB, v.goldenATXID, challenge.PubLayerID, v.layersPerEpoch); err != nil {
		switch err.(type) {
		case *ErrAtxNotFound:
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

	if err := v.validator.InitialNIPostChallenge(challenge.NIPostChallenge, v.atxDB, v.goldenATXID, challenge.InitialPost.Indices); err != nil {
		switch err.(type) {
		case *ErrAtxNotFound:
			return &VerifyError{source: err}
		default:
			return fmt.Errorf("%w: %v", ErrChallengeInvalid, err)
		}
	}

	if err := v.validator.PostMetadata(&v.cfg, challenge.InitialPostMetadata); err != nil {
		return fmt.Errorf("%w: invalid initial Post Metadata: %v", ErrChallengeInvalid, err)
	}

	if err := v.validator.Post(nodeID, *challenge.CommitmentATX, challenge.InitialPost, challenge.InitialPostMetadata, challenge.NumUnits); err != nil {
		return fmt.Errorf("%w: invalid initial Post: %v", ErrChallengeInvalid, err)
	}

	return nil
}

func (v *challengeVerifier) verifyNonInitialChallenge(ctx context.Context, challenge *types.PoetChallenge, nodeID types.NodeID) error {
	if err := v.validator.NIPostChallenge(challenge.NIPostChallenge, v.atxDB, nodeID); err != nil {
		switch err.(type) {
		case *ErrAtxNotFound:
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
