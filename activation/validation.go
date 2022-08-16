package activation

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/verifying"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// Validator contains the dependencies required to validate NIPosts.
type Validator struct {
	poetDb poetDbAPI
	cfg    PostConfig
}

// A compile time check to ensure that Validator fully implements the nipostValidator interface.
var _ nipostValidator = (*Validator)(nil)

// NewValidator returns a new NIPost validator.
func NewValidator(poetDb poetDbAPI, cfg PostConfig) *Validator {
	return &Validator{poetDb, cfg}
}

// Validate validates a NIPost, given a miner id and expected challenge. It returns an error if an issue is found or nil
// if the NIPost is valid.
// Some of the Post metadata fields validation values is ought to eventually be derived from
// consensus instead of local configuration. If so, their validation should be removed to contextual validation,
// while still syntactically-validate them here according to locally configured min/max values.
func (v *Validator) Validate(minerID signing.PublicKey, nipost *types.NIPost, expectedChallenge types.Hash32, numUnits uint) (uint64, error) {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return 0, fmt.Errorf("invalid `Challenge`; expected: %x, given: %x", expectedChallenge, nipost.Challenge)
	}

	if uint(nipost.PostMetadata.BitsPerLabel) < v.cfg.BitsPerLabel {
		return 0, fmt.Errorf("invalid `BitsPerLabel`; expected: >=%d, given: %d", v.cfg.BitsPerLabel, nipost.PostMetadata.BitsPerLabel)
	}

	if nipost.PostMetadata.LabelsPerUnit < uint64(v.cfg.LabelsPerUnit) {
		return 0, fmt.Errorf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", v.cfg.LabelsPerUnit, nipost.PostMetadata.LabelsPerUnit)
	}

	if numUnits < v.cfg.MinNumUnits {
		return 0, fmt.Errorf("invalid `numUnits`; expected: >=%d, given: %d", v.cfg.MinNumUnits, numUnits)
	}

	if numUnits > v.cfg.MaxNumUnits {
		return 0, fmt.Errorf("invalid `numUnits`; expected: <=%d, given: %d", v.cfg.MaxNumUnits, numUnits)
	}

	if uint(nipost.PostMetadata.K1) > v.cfg.K1 {
		return 0, fmt.Errorf("invalid `K1`; expected: <=%d, given: %d", v.cfg.K1, nipost.PostMetadata.K1)
	}

	if uint(nipost.PostMetadata.K2) < v.cfg.K2 {
		return 0, fmt.Errorf("invalid `K2`; expected: >=%d, given: %d", v.cfg.K2, nipost.PostMetadata.K2)
	}

	proof, err := v.poetDb.GetProof(nipost.PostMetadata.Challenge)
	if err != nil {
		return 0, fmt.Errorf("poet proof is not available %x: %w", nipost.PostMetadata.Challenge, err)
	}
	if !isIncluded(proof, nipost.Challenge.Bytes()) {
		return 0, fmt.Errorf("challenge is not included in the proof %x", nipost.PostMetadata.Challenge)
	}

	if err := v.ValidatePost(minerID.Bytes(), nipost.Post, nipost.PostMetadata, numUnits); err != nil {
		return 0, fmt.Errorf("invalid Post: %v", err)
	}

	return proof.LeafCount, nil
}

func isIncluded(proof *types.PoetProof, member []byte) bool {
	for _, part := range proof.Members {
		if bytes.Equal(part, member) {
			return true
		}
	}
	return false
}

// ValidatePost validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) ValidatePost(id []byte, PoST *types.Post, PostMetadata *types.PostMetadata, numUnits uint) error {
	p := (*proving.Proof)(PoST)

	m := new(proving.ProofMetadata)
	m.ID = id
	m.NumUnits = numUnits
	m.Challenge = PostMetadata.Challenge
	m.BitsPerLabel = uint(PostMetadata.BitsPerLabel)
	m.LabelsPerUnit = uint(PostMetadata.LabelsPerUnit)
	m.K1 = uint(PostMetadata.K1)
	m.K2 = uint(PostMetadata.K2)

	if err := verifying.Verify(p, m); err != nil {
		return fmt.Errorf("verify PoST: %w", err)
	}

	return nil
}
