package nipst

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/proving"
)

type verifyPostFunc func(proof *PostProof, space uint64, numberOfProvenLabels uint8, difficulty proving.Difficulty) (bool, error)
type verifyPoetMembershipFunc func(member *common.Hash, proof *MembershipProof) (bool, error)
type verifyPoetFunc func(p *PoetProof) (bool, error)
type verifyPoetMatchesMembershipFunc func(*MembershipProof, *PoetProof) bool

type Validator struct {
	PostParams
	verifyPost                  verifyPostFunc
	verifyPoetMembership        verifyPoetMembershipFunc
	verifyPoet                  verifyPoetFunc
	verifyPoetMatchesMembership verifyPoetMatchesMembershipFunc
}

type PostParams struct {
	Difficulty           proving.Difficulty
	NumberOfProvenLabels uint8
	SpaceUnit            uint64
}

func (v *Validator) Validate(nipst *NIPST, expectedChallenge common.Hash) error {
	return nil
	if !bytes.Equal(nipst.NipstChallenge[:], expectedChallenge[:]) {
		log.Warning("NIPST challenge is not equal to expected challenge")
		return errors.New("NIPST challenge is not equal to expected challenge")
	}

	if valid, err := v.verifyPoetMembership(nipst.NipstChallenge, nipst.PoetMembershipProof); err != nil || !valid {
		log.Warning("PoET membership proof invalid: %v", err)
		return fmt.Errorf("PoET membership proof invalid: %v", err)
	}

	if valid := v.verifyPoetMatchesMembership(nipst.PoetMembershipProof, nipst.PoetProof); !valid {
		log.Warning("PoET membership root does not match PoET proof")
		return errors.New("PoET membership root does not match PoET proof")
	}

	if valid, err := v.verifyPoet(nipst.PoetProof); err != nil || !valid {
		log.Warning("PoET proof invalid: %v", err)
		return fmt.Errorf("PoET proof invalid: %v", err)
	}

	if !bytes.Equal(nipst.PostChallenge[:], nipst.PostProof.Challenge) {
		log.Warning("PoST challenge in NIPST does not match the one in the PoST proof")
		return errors.New("PoST challenge in NIPST does not match the one in the PoST proof")
	}

	if !bytes.Equal(nipst.PostChallenge[:], nipst.PoetProof.Commitment) {
		log.Warning("PoST challenge does not match PoET commitment")
		return errors.New("PoST challenge does not match PoET commitment")
	}

	if !bytes.Equal(nipst.PostProof.Identity, nipst.Id) {
		log.Warning("PoST proof identity does not match NIPST identity")
		return errors.New("PoST proof identity does not match NIPST identity")
	}

	if nipst.Space < v.SpaceUnit {
		log.Warning("PoST space (%d) is less than a single space unit (%d)", nipst.Space, v.SpaceUnit)
		return fmt.Errorf("PoST space (%d) is less than a single space unit (%d)", nipst.Space, v.SpaceUnit)
	}

	if valid, err := v.verifyPost(nipst.PostProof, nipst.Space, v.NumberOfProvenLabels, v.Difficulty); err != nil || !valid {

		log.Warning("PoST proof invalid: %v", err)
		return fmt.Errorf("PoST proof invalid: %v", err)
	}

	return nil
}
