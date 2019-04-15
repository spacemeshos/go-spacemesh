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
type verifyPoetMembershipFunc func(member *common.Hash, proof *membershipProof) (bool, error)
type verifyPoetFunc func(p *poetProof) (bool, error)
type verifyPoetMatchesMembershipFunc func(*membershipProof, *poetProof) bool

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
	if !bytes.Equal(nipst.nipstChallenge[:], expectedChallenge[:]) {
		log.Warning("NIPST challenge is not equal to expected challenge")
		return errors.New("NIPST challenge is not equal to expected challenge")
	}

	if valid, err := v.verifyPoetMembership(nipst.nipstChallenge, nipst.poetMembershipProof); err != nil || !valid {
		log.Warning("PoET membership proof invalid: %v", err)
		return fmt.Errorf("PoET membership proof invalid: %v", err)
	}

	if valid := v.verifyPoetMatchesMembership(nipst.poetMembershipProof, nipst.poetProof); !valid {
		log.Warning("PoET membership root does not match PoET proof")
		return errors.New("PoET membership root does not match PoET proof")
	}

	if valid, err := v.verifyPoet(nipst.poetProof); err != nil || !valid {
		log.Warning("PoET proof invalid: %v", err)
		return fmt.Errorf("PoET proof invalid: %v", err)
	}

	if !bytes.Equal(nipst.postChallenge[:], nipst.postProof.Challenge) {
		log.Warning("PoST challenge in NIPST does not match the one in the PoST proof")
		return errors.New("PoST challenge in NIPST does not match the one in the PoST proof")
	}

	if !bytes.Equal(nipst.postChallenge[:], nipst.poetProof.commitment) {
		log.Warning("PoST challenge does not match PoET commitment")
		return errors.New("PoST challenge does not match PoET commitment")
	}

	if !bytes.Equal(nipst.postProof.Identity, nipst.id) {
		log.Warning("PoST proof identity does not match NIPST identity")
		return errors.New("PoST proof identity does not match NIPST identity")
	}

	if nipst.space < v.SpaceUnit {
		log.Warning("PoST space (%d) is less than a single space unit (%d)", nipst.space, v.SpaceUnit)
		return fmt.Errorf("PoST space (%d) is less than a single space unit (%d)", nipst.space, v.SpaceUnit)
	}

	if valid, err := v.verifyPost(nipst.postProof, nipst.space, v.NumberOfProvenLabels, v.Difficulty); err != nil || !valid {

		log.Warning("PoST proof invalid: %v", err)
		return fmt.Errorf("PoST proof invalid: %v", err)
	}

	return nil
}
