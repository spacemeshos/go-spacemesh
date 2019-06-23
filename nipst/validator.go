package nipst

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/post/proving"
)

type verifyPostFunc func(proof *types.PostProof, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) (bool, error)
type verifyPoetFunc func(p *PoetProof) (bool, error)
type verifyPoetMatchesMembershipFunc func(membershipRoot *common.Hash, poetProof *PoetProof) bool

type Validator struct {
	PostParams
	poetDb                      PoetDb
	verifyPost                  verifyPostFunc
	verifyPoet                  verifyPoetFunc
	verifyPoetMatchesMembership verifyPoetMatchesMembershipFunc
}

type PostParams struct {
	Difficulty           proving.Difficulty
	NumberOfProvenLabels uint8
	SpaceUnit            uint64
}

func NewValidator(params PostParams, poetDb PoetDb) *Validator {
	return &Validator{
		PostParams:                  params,
		poetDb:                      poetDb,
		verifyPost:                  verifyPost,
		verifyPoet:                  verifyPoet,
		verifyPoetMatchesMembership: verifyPoetMatchesMembership,
	}
}

func (v *Validator) Validate(nipst *types.NIPST, expectedChallenge common.Hash) error {
	if !bytes.Equal(nipst.NipstChallenge[:], expectedChallenge[:]) {
		return errors.New("NIPST challenge is not equal to expected challenge")
	}

	if membership, err := v.poetDb.GetMembershipByPoetProofRef(nipst.PostProof.Challenge); err != nil || !membership[*nipst.NipstChallenge] {
		return fmt.Errorf("PoET proof chain invalid: %v", err)
	}

	if nipst.Space < v.SpaceUnit {
		return fmt.Errorf("PoST space (%d) is less than a single space unit (%d)", nipst.Space, v.SpaceUnit)
	}

	if valid, err := v.verifyPost(nipst.PostProof, nipst.Space, v.NumberOfProvenLabels, v.Difficulty); err != nil || !valid {
		return fmt.Errorf("PoST proof invalid: %v", err)
	}

	return nil
}
