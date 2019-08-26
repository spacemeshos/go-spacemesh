package nipst

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/post/config"
)

type verifyPostFunc func(proof *types.PostProof, space uint64, numberOfProvenLabels uint, difficulty uint) (bool, error)

type Validator struct {
	postCfg    *config.Config
	poetDb     PoetDb
	verifyPost verifyPostFunc
}

func NewValidator(postCfg *config.Config, poetDb PoetDb) *Validator {
	return &Validator{
		postCfg:    postCfg,
		verifyPost: verifyPost,
		poetDb:     poetDb,
	}
}

func (v *Validator) Validate(nipst *types.NIPST, expectedChallenge types.Hash32) error {
	if !bytes.Equal(nipst.NipstChallenge[:], expectedChallenge[:]) {
		return errors.New("NIPST challenge is not equal to expected challenge")
	}

	if membership, err := v.poetDb.GetMembershipMap(nipst.PostProof.Challenge); err != nil || !membership[*nipst.NipstChallenge] {
		return fmt.Errorf("PoET proof chain invalid: %v", err)
	}

	if nipst.Space < v.postCfg.SpacePerUnit {
		return fmt.Errorf("PoST space (%d) is less than a single space unit (%d)", nipst.Space, v.postCfg.SpacePerUnit)
	}

	if valid, err := v.verifyPost(nipst.PostProof, nipst.Space, v.postCfg.NumProvenLabels, v.postCfg.Difficulty); err != nil || !valid {
		return fmt.Errorf("PoST proof invalid: %v", err)
	}

	return nil
}
