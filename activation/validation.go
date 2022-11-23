package activation

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/verifying"

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Validator contains the dependencies required to validate NIPosts.
type Validator struct {
	poetDb poetDbAPI
	cfg    atypes.PostConfig
}

// NewValidator returns a new NIPost validator.
func NewValidator(poetDb poetDbAPI, cfg atypes.PostConfig) *Validator {
	return &Validator{poetDb, cfg}
}

// Validate validates a NIPost, given a miner id and expected challenge. It returns an error if an issue is found or nil
// if the NIPost is valid.
// Some of the Post metadata fields validation values is ought to eventually be derived from
// consensus instead of local configuration. If so, their validation should be removed to contextual validation,
// while still syntactically-validate them here according to locally configured min/max values.
func (v *Validator) Validate(commitment []byte, nipost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32) (uint64, error) {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return 0, fmt.Errorf("invalid `Challenge`; expected: %x, given: %x", expectedChallenge, nipost.Challenge)
	}

	if err := validateNumUnits(&v.cfg, numUnits); err != nil {
		return 0, err
	}

	if err := validatePostMetadata(&v.cfg, nipost.PostMetadata); err != nil {
		return 0, err
	}

	proof, err := v.poetDb.GetProof(nipost.PostMetadata.Challenge)
	if err != nil {
		return 0, fmt.Errorf("poet proof is not available %x: %w", nipost.PostMetadata.Challenge, err)
	}
	if !isIncluded(proof, nipost.Challenge.Bytes()) {
		return 0, fmt.Errorf("challenge is not included in the proof %x", nipost.PostMetadata.Challenge)
	}

	if err := validatePost(commitment, nipost.Post, nipost.PostMetadata, numUnits); err != nil {
		return 0, fmt.Errorf("invalid Post: %v", err)
	}

	return proof.LeafCount, nil
}

// ValidatePost validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (*Validator) ValidatePost(commitment []byte, PoST *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error {
	return validatePost(commitment, PoST, PostMetadata, numUnits)
}

func isIncluded(proof *types.PoetProof, member []byte) bool {
	for _, part := range proof.Members {
		if bytes.Equal(part, member) {
			return true
		}
	}
	return false
}

func validateNumUnits(cfg *atypes.PostConfig, numUnits uint32) error {
	if numUnits < cfg.MinNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: >=%d, given: %d", cfg.MinNumUnits, numUnits)
	}

	if numUnits > cfg.MaxNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: <=%d, given: %d", cfg.MaxNumUnits, numUnits)
	}
	return nil
}

func validatePostMetadata(cfg *atypes.PostConfig, metadata *types.PostMetadata) error {
	if metadata.BitsPerLabel < cfg.BitsPerLabel {
		return fmt.Errorf("invalid `BitsPerLabel`; expected: >=%d, given: %d", cfg.BitsPerLabel, metadata.BitsPerLabel)
	}

	if metadata.LabelsPerUnit < uint64(cfg.LabelsPerUnit) {
		return fmt.Errorf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", cfg.LabelsPerUnit, metadata.LabelsPerUnit)
	}

	if metadata.K1 > cfg.K1 {
		return fmt.Errorf("invalid `K1`; expected: <=%d, given: %d", cfg.K1, metadata.K1)
	}

	if metadata.K2 < cfg.K2 {
		return fmt.Errorf("invalid `K2`; expected: >=%d, given: %d", cfg.K2, metadata.K2)
	}
	return nil
}

func validatePost(commitment []byte, PoST *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error {
	p := (*proving.Proof)(PoST)

	m := new(proving.ProofMetadata)
	m.Commitment = commitment
	m.NumUnits = numUnits
	m.Challenge = PostMetadata.Challenge
	m.BitsPerLabel = PostMetadata.BitsPerLabel
	m.LabelsPerUnit = PostMetadata.LabelsPerUnit
	m.K1 = PostMetadata.K1
	m.K2 = PostMetadata.K2

	if err := verifying.Verify(p, m); err != nil {
		return fmt.Errorf("verify PoST: %w", err)
	}

	return nil
}
