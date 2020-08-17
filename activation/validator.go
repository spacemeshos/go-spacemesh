package activation

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/config"
)

// Validator contains the dependencies required to validate NIPSTs
type Validator struct {
	postCfg *config.Config
	poetDb  poetDbAPI
}

// NewValidator returns a new NIPST validator
func NewValidator(postCfg *config.Config, poetDb poetDbAPI) *Validator {
	return &Validator{
		postCfg: postCfg,
		poetDb:  poetDb,
	}
}

// Validate validates a NIPST, given a miner id and expected challenge. It returns an error if an issue is found or nil
// if the NIPST is valid.
func (v *Validator) Validate(minerID signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error {
	if !bytes.Equal(nipst.NipstChallenge[:], expectedChallenge[:]) {
		return errors.New("NIPST challenge is not equal to expected challenge")
	}

	if membership, err := v.poetDb.GetMembershipMap(nipst.PostProof.Challenge); err != nil || !membership[*nipst.NipstChallenge] {
		return fmt.Errorf("PoET proof chain invalid: %v", err)
	}

	if nipst.NumLabels < v.postCfg.NumLabels {
		return fmt.Errorf("invalid NumLabels; expected: >=%d, given: %d", v.postCfg.NumLabels, nipst.NumLabels)
	}

	if err := v.VerifyPoST(nipst.PostProof, minerID, nipst.NumLabels); err != nil {
		return fmt.Errorf("invalid PoST proof: %v", err)
	}

	return nil
}

// VerifyPoST validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) VerifyPoST(proof *types.PostProof, minerID signing.PublicKey, numLabels uint64) error {
	return verifyPoST(proof, minerID.Bytes(), numLabels, v.postCfg.LabelSize, v.postCfg.K1, v.postCfg.K2)
}
