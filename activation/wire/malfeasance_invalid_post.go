package wire

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

// ProofInvalidPost is a proof that an ATX with an invalid Post was published by a smesher.
//
// We are proofing the following:
// 1. The provided Post is invalid for the given SmesherID.
// 2. The ATX has a valid signature.
//
// For this we need additional information:
// 1. The initial ATX of the smesher for the Commitment ATX
// 2. The marriage ATX of the smesher in the case the smesher is part of an equivocation set.
type ProofInvalidPost struct {
	// NodeID is the node ID that created the invalid proof
	NodeID types.NodeID

	Commitment CommitmentProof

	InvalidPost InvalidATXPostProof

	// TODO(mafa): add marriage ATX proof - the marriage index is needed to verify that NodeID created the proof
}

var _ Proof = &ProofInvalidPost{}

func NewInvalidPostProof(atx, initialAtx *ActivationTxV2) (*ProofInvalidPost, error) {
	// TODO(mafa): implement
	return nil, nil
}

// Valid returns true if the proof is valid. It verifies that the two proofs have the same publish epoch, smesher ID,
// and a valid signature but different ATX IDs as well as that the provided merkle proofs are valid.
func (p ProofInvalidPost) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	if err := p.Commitment.Valid(edVerifier, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid commitment proof: %w", err)
	}

	// TODO(mafa): verify p.NodeID to match the ID in the marriage ATX via the marriage index

	// TODO(mafa): nil is not a valid post verifier - replace edVerifier with interface to validation functions
	// (signature and post)
	if err := p.InvalidPost.Valid(edVerifier, nil, p.Commitment.CommitmentATX, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid invalid post proof: %w", err)
	}

	return p.NodeID, nil
}

// CommitmentProof is a proof for the commitment ATX of a smesher. It is generated from the initial ATX of the smesher.
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
	// CommitmentATXProof contains the merkle path from the root of the ATX merkle tree (ATXID) to the CommitmentATX
	// field.
	CommitmentATXProof []types.Hash32 `scale:"max=32"`

	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid and that the merkle proofs
// are valid.
func (p CommitmentProof) Valid(edVerifier *signing.EdVerifier, nodeID types.NodeID) error {
	if !edVerifier.Verify(signing.ATX, nodeID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid signature")
	}

	if p.InitialPostRoot == types.EmptyHash32 {
		return errors.New("invalid initial PoST root") // initial PoST root is empty for non-initial ATXs
	}

	initialPostProof := make([][]byte, len(p.InitialPostProof))
	for i, h := range p.InitialPostProof {
		initialPostProof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(InitialPostsRootIndex)},
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

	proof := make([][]byte, len(p.CommitmentATXProof))
	for i, h := range p.CommitmentATXProof {
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
	// PostsRootProof contains the merkle path from the NiPostsRoot to the PostsRoot field.
	PostsRootProof []types.Hash32 `scale:"max=32"`

	// SubPostRoot is the root of the sub PoST merkle tree.
	SubPostRoot types.Hash32
	// SubPostRootIndex is the index of the sub PoST in the NiPoST.
	SubPostRootIndex uint64
	// SubPostRootProof contains the merkle path from the PostsRoot to the SubPostRoot field.
	SubPostRootProof []types.Hash32 `scale:"max=32"`

	// Post is the invalid PoST.
	Post PostV1
	// PostProof contains the merkle path from the SubPostRoot to the PoST field.
	PostProof []types.Hash32 `scale:"max=32"`
	// NumUnits is the number of units in the PoST.
	NumUnits uint32
	// NumUnitsProof contains the merkle path from the PoST to the NumUnits field.
	NumUnitsProof []types.Hash32 `scale:"max=32"`
	// InvalidPostIndex is the index of the leaf that was identified to be invalid.
	InvalidPostIndex uint32

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid, that the merkle proofs are
// and that the provided post is invalid.
func (p InvalidATXPostProof) Valid(
	edVerifier *signing.EdVerifier,
	validator postVerifier,
	commitmentATX types.ATXID,
	nodeID types.NodeID,
) error {
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

	postsProof := make([][]byte, len(p.PostsRootProof))
	for i, h := range p.PostsRootProof {
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

	subPostProof := make([][]byte, len(p.SubPostRootProof))
	for i, h := range p.SubPostRootProof {
		subPostProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{p.SubPostRootIndex},
		[][]byte{p.SubPostRoot.Bytes()},
		subPostProof,
		p.PostsRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate sub PoST root proof: %w", err)
	}
	if !ok {
		return errors.New("invalid sub PoST root proof")
	}

	postProof := make([][]byte, len(p.PostProof))
	for i, h := range p.PostProof {
		postProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(PostIndex)},
		[][]byte{p.Post.Root()},
		postProof,
		p.SubPostRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate PoST proof: %w", err)
	}
	if !ok {
		return errors.New("invalid PoST proof")
	}

	numUnits := make([]byte, 4)
	binary.LittleEndian.PutUint32(numUnits, p.NumUnits)

	numUnitsProof := make([][]byte, len(p.NumUnitsProof))
	for i, h := range p.NumUnitsProof {
		numUnitsProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(NumUnitsIndex)},
		[][]byte{numUnits},
		numUnitsProof,
		p.Post.Root(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate PoST num units proof: %w", err)
	}
	if !ok {
		return errors.New("invalid PoST num units proof")
	}

	if err := validator.PostV2Idx(
		context.Background(),
		nodeID,
		commitmentATX,
		PostFromWireV1(&p.Post),
		p.Challenge.Bytes(),
		p.NumUnits,
		int(p.InvalidPostIndex),
	); err != nil {
		return nil
	}
	return errors.New("PoST is valid")
}
