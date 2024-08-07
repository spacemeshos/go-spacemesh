package wire

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

// ProofInvalidPost is a proof that an ATXs with an invalid Post was published by a smesher.
//
// We are proofing the following:
// 1. The provided Post is invalid for the given SmesherID.
// 2. The ATX has a valid signature.
//
// For this we need additional information:
// 1. The initial ATX of the smesher for the Commitment ATX
// 2. The marriage ATX of the smesher in the case the smesher is part of an equivocation set.
type ProofInvalidPost struct {
	InvalidPost InvalidATXPostProof
	Commitment  CommitmentProof

	// TODO(mafa): add marriage ATX proof
}

var _ Proof = &ProofInvalidPost{}

func NewInvalidPostProof(atx, initialAtx *ActivationTxV2) (*ProofInvalidPost, error) {
	// TODO(mafa): implement
	return nil, nil
}

// Valid returns true if the proof is valid. It verifies that the two proofs have the same publish epoch, smesher ID,
// and a valid signature but different ATX IDs as well as that the provided merkle proofs are valid.
func (p ProofInvalidPost) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	// TODO(mafa): implement
	return types.EmptyNodeID, nil
}

type CommitmentProof struct {
	// ATXID is the ID of the ATX being proven. It is the merkle root from the contents of the ATX.
	ATXID types.ATXID

	// InitialPostRoot is the root of the initial PoST merkle tree.
	InitialPostRoot types.Hash32

	// InitialPostProof contains the merkle path from the root of the ATX merkle tree (ATXID) to the root of the
	// InitialPost.
	InitialPostProof []types.Hash32 `scale:"max=32"`

	// CommitmentATX is the ATX that was used by the identity as their commitment ATX.
	CommitmentATX types.ATXID

	// Proof contains the merkle path from the root of the ATX merkle tree (ATXID) to the CommitmentATX field.
	Proof []types.Hash32 `scale:"max=32"`

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid and that the merkle proofs
// are valid.
func (p CommitmentProof) Valid(edVerifier *signing.EdVerifier) error {
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid signature")
	}

	initialPostProof := make([][]byte, len(p.InitialPostProof))
	for i, h := range p.InitialPostProof {
		initialPostProof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(InitialPostIndex)},
		[][]byte{p.InitialPostRoot.Bytes()},
		initialPostProof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate initial PoST proof: %w", err)
	}
	if !ok {
		return errors.New("invalid initial PoST proof")
	}

	proof := make([][]byte, len(p.Proof))
	for i, h := range p.Proof {
		proof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(CommitmentATXIndex)},
		[][]byte{p.CommitmentATX.Bytes()},
		proof,
		p.InitialPostRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate commitment ATX proof: %w", err)
	}
	if !ok {
		return errors.New("invalid commitment ATX proof")
	}

	return nil
}

type InvalidATXPostProof struct {
	// ATXID is the ID of the ATX being proven. It is the merkle root from the contents of the ATX.
	ATXID types.ATXID

	// NiPostsRoot is the root of the NiPoST merkle tree.
	NiPostsRoot types.Hash32
	// NiPostsProof contains the merkle path from the root of the ATX merkle tree (ATXID) to the Post field.
	NiPostsProof []types.Hash32 `scale:"max=32"`

	// Challenge for the NiPoST.
	Challenge types.Hash32
	// ChallengeProof contains the merkle path from the NiPostsRoot to the Challenge field.
	ChallengeProof []types.Hash32 `scale:"max=32"`

	// PostsRoot is the root of the PoST merkle tree.
	PostsRoot types.Hash32
	// PostsProof contains the merkle path from the NiPostsRoot to the PostsRoot field.
	PostsProof []types.Hash32 `scale:"max=32"`

	// TODO(mafa): include invalid post and merkle proof for it

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid, that the merkle proofs are
// and that the provided post is invalid.
func (p InvalidATXPostProof) Valid(edVerifier *signing.EdVerifier) error {
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid signature")
	}

	nipostsProof := make([][]byte, len(p.NiPostsProof))
	for i, h := range p.NiPostsProof {
		nipostsProof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(NIPostsRootIndex)},
		[][]byte{p.NiPostsRoot.Bytes()},
		nipostsProof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate NiPoST root proof: %w", err)
	}
	if !ok {
		return errors.New("invalid NiPoST root proof")
	}

	challengeProof := make([][]byte, len(p.ChallengeProof))
	for i, h := range p.ChallengeProof {
		challengeProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(ChallengeIndex)},
		[][]byte{p.Challenge.Bytes()},
		challengeProof,
		p.NiPostsRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate NiPoST challenge proof: %w", err)
	}
	if !ok {
		return errors.New("invalid NiPoST challenge proof")
	}

	postsProof := make([][]byte, len(p.PostsProof))
	for i, h := range p.PostsProof {
		postsProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(PostsRootIndex)},
		[][]byte{p.PostsRoot.Bytes()},
		postsProof,
		p.NiPostsRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate PoST root proof: %w", err)
	}
	if !ok {
		return errors.New("invalid PoST root proof")
	}

	// TODO(mafa): continue with validation of provided post

	return nil
}
