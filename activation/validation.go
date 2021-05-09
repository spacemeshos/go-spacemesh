package activation

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/verifying"
)

// Validator contains the dependencies required to validate NIPoSTs
type Validator struct {
	poetDb  poetDbAPI
	postCfg config.Config
}

// A compile time check to ensure that Validator fully implements the NIPoSTValidator interface.
var _ NIPoSTValidator = (*Validator)(nil)

// NewValidator returns a new NIPoST validator
func NewValidator(poetDb poetDbAPI, postCfg config.Config) *Validator {
	return &Validator{poetDb, postCfg}
}

// Validate validates a NIPoST, given a miner id and expected challenge. It returns an error if an issue is found or nil
// if the NIPoST is valid.
// Some of the PoST metadata fields validation values is ought to eventually be derived from
// consensus instead of local configuration. If so, their validation should be removed to contextual validation,
// while still syntactically-validate them here according to locally configured min/max values.
func (v *Validator) Validate(minerID signing.PublicKey, nipost *types.NIPoST, expectedChallenge types.Hash32, numUnits uint) error {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return fmt.Errorf("invalid `Challenge`; expected: %v, given: %v", expectedChallenge, nipost.Challenge)
	}

	if nipost.PoSTMetadata.BitsPerLabel < v.postCfg.BitsPerLabel {
		return fmt.Errorf("invalid `BitsPerLabel`; expected: >=%d, given: %d", v.postCfg.BitsPerLabel, nipost.PoSTMetadata.BitsPerLabel)
	}

	if nipost.PoSTMetadata.LabelsPerUnit < v.postCfg.LabelsPerUnit {
		return fmt.Errorf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", v.postCfg.LabelsPerUnit, nipost.PoSTMetadata.LabelsPerUnit)
	}

	if numUnits < v.postCfg.MinNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: >=%d, given: %d", v.postCfg.MinNumUnits, numUnits)
	}

	if numUnits > v.postCfg.MaxNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: <=%d, given: %d", v.postCfg.MaxNumUnits, numUnits)
	}

	if nipost.PoSTMetadata.K1 > v.postCfg.K1 {
		return fmt.Errorf("invalid `K1`; expected: <=%d, given: %d", v.postCfg.K1, nipost.PoSTMetadata.K1)
	}

	if nipost.PoSTMetadata.K2 < v.postCfg.K2 {
		return fmt.Errorf("invalid `K2`; expected: >=%d, given: %d", v.postCfg.K2, nipost.PoSTMetadata.K2)
	}

	// TODO: better error handling
	if membership, err := v.poetDb.GetMembershipMap(nipost.PoSTMetadata.Challenge); err != nil || !membership[*nipost.Challenge] {
		return fmt.Errorf("invalid PoET chain: %v", err)
	}

	if err := v.ValidatePoST(minerID.Bytes(), nipost.PoST, nipost.PoSTMetadata, numUnits); err != nil {
		return fmt.Errorf("invalid PoST: %v", err)
	}

	return nil
}

// ValidatePoST validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) ValidatePoST(id []byte, PoST *types.PoST, PoSTMetadata *types.PoSTMetadata, numUnits uint) error {
	p := (*proving.Proof)(PoST)

	m := new(proving.ProofMetadata)
	m.ID = id
	m.NumUnits = numUnits
	m.Challenge = PoSTMetadata.Challenge
	m.BitsPerLabel = PoSTMetadata.BitsPerLabel
	m.LabelsPerUnit = PoSTMetadata.LabelsPerUnit
	m.K1 = PoSTMetadata.K1
	m.K2 = PoSTMetadata.K2

	return verifying.Verify(p, m)
}
