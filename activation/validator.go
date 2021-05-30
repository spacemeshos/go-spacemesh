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
func (v *Validator) Validate(minerID signing.PublicKey, nipst *types.NIPST, space uint64, expectedChallenge types.Hash32) error {
	if !bytes.Equal(nipst.NipstChallenge[:], expectedChallenge[:]) {
		return errors.New("NIPST challenge is not equal to expected challenge")
	}

	if membership, err := v.poetDb.GetMembershipMap(nipst.PostProof.Challenge); err != nil || !membership[*nipst.NipstChallenge] {
		return fmt.Errorf("PoET proof chain invalid: %v", err)
	}

	// TODO: This validation should be in SyntacticallyValidateAtx and SpacePerUnit should not be in the postCfg.
	if space < v.postCfg.SpacePerUnit {
		return fmt.Errorf("PoST space (%d) is less than a single space unit (%d)", space, v.postCfg.SpacePerUnit)
	}

	if err := v.VerifyPost(minerID, nipst.PostProof, space); err != nil {
		return fmt.Errorf("PoST proof invalid: %v", err)
	}

	return nil
}

// VerifyPost validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) VerifyPost(minerID signing.PublicKey, proof *types.PostProof, space uint64) error {
	return verifyPost(minerID, proof, space, v.postCfg.NumProvenLabels, v.postCfg.Difficulty)
}
