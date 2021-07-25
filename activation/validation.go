package activation

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/verifying"
)

// Validator contains the dependencies required to validate NIPosts
type Validator struct {
	poetDb poetDbAPI
	cfg    PostConfig
}

// A compile time check to ensure that Validator fully implements the nipostValidator interface.
var _ nipostValidator = (*Validator)(nil)

// NewValidator returns a new NIPost validator
func NewValidator(poetDb poetDbAPI, cfg PostConfig) *Validator {
	return &Validator{poetDb, cfg}
}

// Validate validates a NIPost, given a miner id and expected challenge. It returns an error if an issue is found or nil
// if the NIPost is valid.
// Some of the Post metadata fields validation values is ought to eventually be derived from
// consensus instead of local configuration. If so, their validation should be removed to contextual validation,
// while still syntactically-validate them here according to locally configured min/max values.
func (v *Validator) Validate(minerID signing.PublicKey, nipost *types.NIPost, expectedChallenge types.Hash32, numUnits uint) error {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return fmt.Errorf("invalid `Challenge`; expected: %x, given: %x", expectedChallenge, nipost.Challenge)
	}

	if nipost.PostMetadata.BitsPerLabel < v.cfg.BitsPerLabel {
		return fmt.Errorf("invalid `BitsPerLabel`; expected: >=%d, given: %d", v.cfg.BitsPerLabel, nipost.PostMetadata.BitsPerLabel)
	}

	if nipost.PostMetadata.LabelsPerUnit < v.cfg.LabelsPerUnit {
		return fmt.Errorf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", v.cfg.LabelsPerUnit, nipost.PostMetadata.LabelsPerUnit)
	}

	if numUnits < v.cfg.MinNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: >=%d, given: %d", v.cfg.MinNumUnits, numUnits)
	}

	if numUnits > v.cfg.MaxNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: <=%d, given: %d", v.cfg.MaxNumUnits, numUnits)
	}

	if nipost.PostMetadata.K1 > v.cfg.K1 {
		return fmt.Errorf("invalid `K1`; expected: <=%d, given: %d", v.cfg.K1, nipost.PostMetadata.K1)
	}

	if nipost.PostMetadata.K2 < v.cfg.K2 {
		return fmt.Errorf("invalid `K2`; expected: >=%d, given: %d", v.cfg.K2, nipost.PostMetadata.K2)
	}

	// TODO: better error handling
	if membership, err := v.poetDb.GetMembershipMap(nipost.PostMetadata.Challenge); err != nil || !membership[*nipost.Challenge] {
		return fmt.Errorf("invalid PoET chain: %v", err)
	}

	if err := v.ValidatePost(minerID.Bytes(), nipost.Post, nipost.PostMetadata, numUnits); err != nil {
		return fmt.Errorf("invalid Post: %v", err)
	}

	return nil
}

// ValidatePost validates a Proof of Space-Time (Post). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) ValidatePost(id []byte, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint) error {
	p := (*proving.Proof)(Post)

	m := new(proving.ProofMetadata)
	m.ID = id
	m.NumUnits = numUnits
	m.Challenge = PostMetadata.Challenge
	m.BitsPerLabel = PostMetadata.BitsPerLabel
	m.LabelsPerUnit = PostMetadata.LabelsPerUnit
	m.K1 = PostMetadata.K1
	m.K2 = PostMetadata.K2

	return verifying.Verify(p, m)
}
