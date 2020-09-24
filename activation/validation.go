package activation

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/validation"
)

// Validator contains the dependencies required to validate NIPoSTs
type Validator struct {
	poetDb  poetDbAPI
	postCfg config.Config
}

// NewValidator returns a new NIPoST validator
func NewValidator(poetDb poetDbAPI, postCfg config.Config) *Validator {
	return &Validator{poetDb, postCfg}
}

// Validate validates a NIPoST, given a miner id and expected challenge. It returns an error if an issue is found or nil
// if the NIPoST is valid.
func (v *Validator) Validate(minerID signing.PublicKey, nipost *types.NIPoST, expectedChallenge types.Hash32) error {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return errors.New("nipost challenge is not equal to expected challenge")
	}

	if membership, err := v.poetDb.GetMembershipMap(nipost.PoSTMetadata.Challenge); err != nil || !membership[*nipost.Challenge] {
		return fmt.Errorf("PoET proof chain invalid: %v", err)
	}

	if err := v.ValidatePoST(minerID.Bytes(), nipost.PoST, nipost.PoSTMetadata); err != nil {
		return fmt.Errorf("invalid PoST: %v", err)
	}

	// Validate the declared PoST metadata according to locally configured values.
	// Once these values would be derived from consensus instead of local configuration,
	// this validation should be removed to the contextual validation, while still
	// keeping here the validation of min/max values according to local configuration.

	if nipost.PoSTMetadata.NumLabels < v.postCfg.NumLabels {
		return fmt.Errorf("invalid NumLabels; expected: >=%d, given: %d", v.postCfg.NumLabels, nipost.PoSTMetadata.NumLabels)
	}

	if nipost.PoSTMetadata.LabelSize < v.postCfg.LabelSize {
		return fmt.Errorf("invalid LabelSize; expected: >=%d, given: %d", v.postCfg.LabelSize, nipost.PoSTMetadata.LabelSize)
	}

	if nipost.PoSTMetadata.K1 > v.postCfg.K1 {
		return fmt.Errorf("invalid K1; expected: <=%d, given: %d", v.postCfg.K1, nipost.PoSTMetadata.K1)
	}

	if nipost.PoSTMetadata.K2 < v.postCfg.K2 {
		return fmt.Errorf("invalid K2; expected: >=%d, given: %d", v.postCfg.K2, nipost.PoSTMetadata.K2)
	}

	return nil
}

// ValidatePoST validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) ValidatePoST(id []byte, PoST *types.PoST, PoSTMetadata *types.PoSTMetadata) error {
	return validation.Validate(id, (*proving.Proof)(PoST), (*proving.ProofMetadata)(PoSTMetadata))
}
