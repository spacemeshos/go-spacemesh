package wire

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate scalegen

// ProofMergedInvalidPost is a proof that a merged ATX with an invalid Post was published by a smesher.
//
// We are proofing the following:
// 1. The provided Post is invalid for the given SmesherID.
// 2. The ATX has a valid signature.
//
// For this we need additional information:
// 1. The initial ATX of the smesher for the Commitment ATX
// 2. The marriage ATX of the smesher in the case the smesher is part of an equivocation set.
type ProofMergedInvalidPost struct {
	// ATXID is the ID of the ATX containing the invalid PoST.
	ATXID types.ATXID
	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature

	// NodeID is the node ID that created the invalid PoST.
	NodeID types.NodeID

	// MarryProof is the proof for the marriage ATX of the smesher. It proofs that NodeID agreed to marry the signer
	// of the ATX.
	MarryProof MarryProof

	// CommitmentProof is the proof for the commitment ATX of the smesher. Generated from the initial ATX of `NodeID`.
	CommitmentProof CommitmentProof

	// InvalidPostProof is the proof for the invalid PoST of the ATX. It contains the PoST and the merkle proofs to
	// verify the PoST.
	InvalidPostProof InvalidPostProof
}

var _ Proof = &ProofMergedInvalidPost{}

func NewMergedInvalidPostProof(
	db sql.Executor,
	atx, marriageATX, initialATX *ActivationTxV2,
	nodeID types.NodeID,
	nipostIndex int,
) (*ProofMergedInvalidPost, error) {
	marriageProof, err := createMarryProof(db, marriageATX, nodeID)
	if err != nil {
		return nil, fmt.Errorf("marriage proof: %w", err)
	}

	commitmentProof, err := createCommitmentProof(initialATX, nodeID)
	if err != nil {
		return nil, fmt.Errorf("commitment proof: %w", err)
	}

	invalidPostProof, err := createInvalidPostProof(atx, nipostIndex, int(marriageProof.CertificateIndex))
	if err != nil {
		return nil, fmt.Errorf("invalid post proof: %w", err)
	}

	proof := &ProofMergedInvalidPost{
		ATXID:     atx.ID(),
		SmesherID: atx.SmesherID,
		Signature: atx.Signature,

		NodeID: nodeID,

		MarryProof:       marriageProof,
		CommitmentProof:  commitmentProof,
		InvalidPostProof: invalidPostProof,
	}
	return proof, nil
}

func (p ProofMergedInvalidPost) Valid(ctx context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if !malValidator.Signature(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return types.EmptyNodeID, errors.New("invalid signature")
	}

	if err := p.MarryProof.Valid(malValidator, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid marriage proof: %w", err)
	}

	if err := p.CommitmentProof.Valid(malValidator, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid commitment proof: %w", err)
	}

	if err := p.InvalidPostProof.Valid(
		ctx,
		malValidator,
		p.ATXID,
		p.NodeID,
		p.CommitmentProof.CommitmentATX,
		p.MarryProof.ATXID,
		p.MarryProof.CertificateIndex,
	); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid invalid post proof: %w", err)
	}

	return p.NodeID, nil
}

// CommitmentProof is a proof for the commitment ATX of a smesher. It is generated from the initial ATX.
type CommitmentProof struct {
	// InitialATXID is the ID of the initial ATX of the smesher.
	InitialATXID types.ATXID

	// InitialPostRoot and its proof that it is contained in the InitialATX.
	InitialPostRoot  InitialPostRoot
	InitialPostProof InitialPostRootProof `scale:"max=32"`

	// CommitmentATX and its proof that it is contained in the InitialPostRoot.
	CommitmentATX      types.ATXID
	CommitmentATXProof CommitmentATXProof `scale:"max=32"`

	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

func createCommitmentProof(initialAtx *ActivationTxV2, nodeID types.NodeID) (CommitmentProof, error) {
	if initialAtx.SmesherID != nodeID {
		return CommitmentProof{}, errors.New("node ID does not match smesher ID of initial ATX")
	}

	if initialAtx.Initial == nil {
		return CommitmentProof{}, errors.New("initial ATX does not contain initial PoST")
	}

	proof := CommitmentProof{
		InitialATXID: initialAtx.ID(),

		InitialPostRoot:  initialAtx.Initial.Root(),
		InitialPostProof: initialAtx.InitialPostRootProof(),

		CommitmentATX:      initialAtx.Initial.CommitmentATX,
		CommitmentATXProof: initialAtx.Initial.CommitmentATXProof(),

		Signature: initialAtx.Signature,
	}
	return proof, nil
}

func (p CommitmentProof) Valid(malValidator MalfeasanceValidator, nodeID types.NodeID) error {
	if !malValidator.Signature(signing.ATX, nodeID, p.InitialATXID.Bytes(), p.Signature) {
		return errors.New("invalid signature")
	}

	if types.Hash32(p.InitialPostRoot) == types.EmptyHash32 {
		return errors.New("invalid empty initial PoST root") // initial PoST root is empty for non-initial ATXs
	}

	if !p.InitialPostProof.Valid(p.InitialATXID, p.InitialPostRoot) {
		return errors.New("invalid initial PoST proof")
	}

	if !p.CommitmentATXProof.Valid(p.InitialPostRoot, p.CommitmentATX) {
		return errors.New("invalid commitment ATX proof")
	}

	return nil
}

// InvalidPostProof is a proof for an invalid PoST in an ATX. It contains the PoST and the merkle proofs to verify the
// PoST.
type InvalidPostProof struct {
	// MarriageATXProof that the ATX contains the MarriageATX from the MarryProof.
	MarriageATXProof MarriageATXProof `scale:"max=32"`

	// NIPostsRoot and its proof that it is contained in the ATX.
	NIPostsRoot      NIPostsRoot
	NIPostsRootProof NIPostsRootProof `scale:"max=32"`

	// NIPostRoot and its proof that it is contained at the given index in the NIPostsRoot.
	NIPostRoot      NIPostRoot
	NIPostRootProof NIPostRootProof `scale:"max=32"`
	NIPostIndex     uint16

	// Challenge and its proof that it is contained in the NIPostRoot.
	Challenge      types.Hash32
	ChallengeProof ChallengeProof `scale:"max=32"`

	// SubPostsRoot and its proof that it is contained in the NIPostRoot.
	SubPostsRoot      SubPostsRoot
	SubPostsRootProof SubPostsRootProof `scale:"max=32"`

	// SubPostRoot and its proof that is contained at the given index in the SubPostsRoot.
	SubPostRoot      SubPostRoot
	SubPostRootProof SubPostRootProof `scale:"max=32"`
	SubPostRootIndex uint16

	// MarriageIndexProof is the proof that the MarriageIndex (CertificateIndex from MarryProof) is contained in the
	// SubPostRoot.
	MarriageIndexProof MarriageIndexProof `scale:"max=32"`

	// Post is the invalid PoST and its proof that it is contained in the SubPostRoot.
	Post      PostV1
	PostProof PostRootProof `scale:"max=32"`

	// NumUnits is the number of units in the PoST.
	NumUnits uint32
	// NumUnitsProof contains the merkle path from the PoST to the NumUnits field.
	NumUnitsProof []types.Hash32 `scale:"max=32"`

	// InvalidPostIndex is the index of the leaf that was identified to be invalid.
	InvalidPostIndex uint32
}

func createInvalidPostProof(atx *ActivationTxV2, nipostIndex, marriageIndex int) (InvalidPostProof, error) {
	if nipostIndex < 0 || nipostIndex >= len(atx.NIPosts) {
		return InvalidPostProof{}, errors.New("invalid NIPoST index")
	}

	postIndex := slices.IndexFunc(atx.NIPosts[nipostIndex].Posts, func(post SubPostV2) bool {
		return post.MarriageIndex == uint32(marriageIndex)
	})
	if postIndex == -1 {
		return InvalidPostProof{}, fmt.Errorf("does not contain PoST with marriage index %d", marriageIndex)
	}

	proof := InvalidPostProof{
		MarriageATXProof: atx.MarriageATXProof(),

		NIPostsRoot:      atx.NIPosts.Root(atx.PreviousATXs),
		NIPostsRootProof: atx.NIPostsRootProof(),

		NIPostRoot:      atx.NIPosts[nipostIndex].Root(atx.PreviousATXs),
		NIPostRootProof: atx.NIPosts.Proof(int(nipostIndex), atx.PreviousATXs),
		NIPostIndex:     uint16(nipostIndex),

		Challenge:      atx.NIPosts[nipostIndex].Challenge,
		ChallengeProof: atx.NIPosts[nipostIndex].ChallengeProof(atx.PreviousATXs),

		SubPostsRoot:      atx.NIPosts[nipostIndex].Posts.Root(atx.PreviousATXs),
		SubPostsRootProof: atx.NIPosts[nipostIndex].PostsRootProof(atx.PreviousATXs),

		SubPostRoot:      atx.NIPosts[nipostIndex].Posts[postIndex].Root(atx.PreviousATXs),
		SubPostRootProof: atx.NIPosts[nipostIndex].Posts.Proof(postIndex, atx.PreviousATXs),
		SubPostRootIndex: uint16(postIndex),

		MarriageIndexProof: atx.NIPosts[nipostIndex].Posts[postIndex].MarriageIndexProof(atx.PreviousATXs),

		Post:      atx.NIPosts[nipostIndex].Posts[postIndex].Post,
		PostProof: atx.NIPosts[nipostIndex].Posts[postIndex].PostProof(atx.PreviousATXs),

		// TODO(mafa): continue with proof
	}
	return proof, nil
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid, that the merkle proofs are
// and that the provided post is invalid.
func (p InvalidPostProof) Valid(
	ctx context.Context,
	malValidator MalfeasanceValidator,
	atxID types.ATXID,
	nodeID types.NodeID,
	commitmentATX types.ATXID,
	marriageATX types.ATXID,
	marriageIndex uint32,
) error {
	if !p.MarriageATXProof.Valid(atxID, marriageATX) {
		return errors.New("invalid marriage ATX proof")
	}

	if !p.NIPostsRootProof.Valid(atxID, p.NIPostsRoot) {
		return errors.New("invalid NIPosts root proof")
	}

	if !p.NIPostRootProof.Valid(p.NIPostsRoot, int(p.NIPostIndex), p.NIPostRoot) {
		return errors.New("invalid NIPoST root proof")
	}

	if !p.ChallengeProof.Valid(p.NIPostRoot, p.Challenge) {
		return errors.New("invalid challenge proof")
	}

	if !p.SubPostsRootProof.Valid(p.NIPostRoot, p.SubPostsRoot) {
		return errors.New("invalid sub PoSTs root proof")
	}

	if !p.SubPostRootProof.Valid(p.SubPostsRoot, int(p.SubPostRootIndex), p.SubPostRoot) {
		return errors.New("invalid sub PoST root proof")
	}

	if !p.MarriageIndexProof.Valid(p.SubPostRoot, marriageIndex) {
		return errors.New("invalid marriage index proof")
	}

	if !p.PostProof.Valid(p.SubPostRoot, p.Post.Root()) {
		return errors.New("invalid PoST proof")
	}

	// TODO(mafa): continue with proof

	numUnits := make([]byte, 4)
	binary.LittleEndian.PutUint32(numUnits, p.NumUnits)

	numUnitsProof := make([][]byte, len(p.NumUnitsProof))
	for i, h := range p.NumUnitsProof {
		numUnitsProof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(NumUnitsIndex)},
		[][]byte{numUnits},
		numUnitsProof,
		types.Hash32(p.Post.Root()).Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate PoST num units proof: %w", err)
	}
	if !ok {
		return errors.New("invalid PoST num units proof")
	}

	if err := malValidator.PostIndex(
		ctx,
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
