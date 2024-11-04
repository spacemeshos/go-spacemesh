package wire

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate scalegen

// ProofInvalidPost is a proof that a merged ATX with an invalid Post was published by a smesher.
//
// We are proofing the following:
// 1. The ATX has a valid signature.
// 2. If NodeID is different from SmesherID, we prove that NodeID and SmesherID are married.
// 3. The commitment ATX of NodeID used for the invalid PoST based on their initial ATX.
// 4. The provided Post is invalid for the given NodeID.
type ProofInvalidPost struct {
	// ATXID is the ID of the ATX containing the invalid PoST.
	ATXID types.ATXID
	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature

	// NodeID is the node ID that created the invalid PoST.
	NodeID types.NodeID

	// MarriageProof is the proof that NodeID and SmesherID are married. It is nil if NodeID == SmesherID.
	MarriageProof *MarriageProof
	// CommitmentProof is the proof for the commitment ATX of the smesher. Generated from the initial ATX of NodeID.
	CommitmentProof CommitmentProof
	// InvalidPostProof is the proof for the invalid PoST of the ATX. It contains the PoST and the merkle proofs to
	// verify the PoST.
	InvalidPostProof InvalidPostProof
}

var _ Proof = &ProofInvalidPost{}

func NewInvalidPostProof(
	db sql.Executor,
	atx, initialATX *ActivationTxV2,
	nodeID types.NodeID,
	nipostIndex int,
	invalidPostIndex uint32,
) (*ProofInvalidPost, error) {
	if atx.SmesherID != nodeID && atx.MarriageATX == nil {
		return nil, errors.New("ATX is not a merged ATX, but NodeID is different from SmesherID")
	}

	if nipostIndex < 0 || nipostIndex >= len(atx.NIPosts) {
		return nil, errors.New("invalid NIPoST index")
	}

	postIndex := 0
	var marriageProof *MarriageProof
	if atx.SmesherID != nodeID {
		proof, err := createMarriageProof(db, atx, nodeID)
		if err != nil {
			return nil, fmt.Errorf("marriage proof: %w", err)
		}
		marriageProof = &proof
		postIndex = slices.IndexFunc(atx.NIPosts[nipostIndex].Posts, func(post SubPostV2) bool {
			return post.MarriageIndex == proof.NodeIDMarryProof.CertificateIndex
		})
		if postIndex == -1 {
			return nil, fmt.Errorf("no PoST from %s in ATX", nodeID.ShortString())
		}
	}

	commitmentProof, err := createCommitmentProof(initialATX, nodeID)
	if err != nil {
		return nil, fmt.Errorf("commitment proof: %w", err)
	}
	invalidPostProof, err := createInvalidPostProof(atx, nipostIndex, postIndex, invalidPostIndex)
	if err != nil {
		return nil, fmt.Errorf("invalid post proof: %w", err)
	}

	return &ProofInvalidPost{
		ATXID:     atx.ID(),
		SmesherID: atx.SmesherID,
		Signature: atx.Signature,

		NodeID: nodeID,

		MarriageProof: marriageProof,

		CommitmentProof:  commitmentProof,
		InvalidPostProof: invalidPostProof,
	}, nil
}

func (p ProofInvalidPost) Valid(ctx context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if !malValidator.Signature(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return types.EmptyNodeID, errors.New("invalid signature")
	}

	if p.NodeID != p.SmesherID && p.MarriageProof == nil {
		return types.EmptyNodeID, errors.New("missing marriage proof")
	}

	var marriageIndex *uint32
	if p.MarriageProof != nil {
		if err := p.MarriageProof.Valid(malValidator, p.ATXID, p.NodeID, p.SmesherID); err != nil {
			return types.EmptyNodeID, fmt.Errorf("invalid marriage proof: %w", err)
		}
		marriageIndex = &p.MarriageProof.NodeIDMarryProof.CertificateIndex
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
		marriageIndex,
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

	return CommitmentProof{
		InitialATXID: initialAtx.ID(),

		InitialPostRoot:  initialAtx.Initial.Root(),
		InitialPostProof: initialAtx.InitialPostRootProof(),

		CommitmentATX:      initialAtx.Initial.CommitmentATX,
		CommitmentATXProof: initialAtx.Initial.CommitmentATXProof(),

		Signature: initialAtx.Signature,
	}, nil
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

	// NumUnits and its proof that it is contained in the SubPostRoot.
	NumUnits      uint32
	NumUnitsProof NumUnitsProof `scale:"max=32"`

	// InvalidPostIndex is the index of the leaf that was identified to be invalid.
	InvalidPostIndex uint32
}

func createInvalidPostProof(
	atx *ActivationTxV2,
	nipostIndex,
	postIndex int,
	invalidPostIndex uint32,
) (InvalidPostProof, error) {
	if nipostIndex < 0 || nipostIndex >= len(atx.NIPosts) {
		return InvalidPostProof{}, errors.New("invalid NIPoST index")
	}
	if postIndex < 0 || postIndex >= len(atx.NIPosts[nipostIndex].Posts) {
		return InvalidPostProof{}, errors.New("invalid PoST index")
	}

	return InvalidPostProof{
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

		NumUnits:      atx.NIPosts[nipostIndex].Posts[postIndex].NumUnits,
		NumUnitsProof: atx.NIPosts[nipostIndex].Posts[postIndex].NumUnitsProof(atx.PreviousATXs),

		InvalidPostIndex: invalidPostIndex,
	}, nil
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid, that the merkle proofs are
// and that the provided post is invalid.
func (p InvalidPostProof) Valid(
	ctx context.Context,
	malValidator MalfeasanceValidator,
	atxID types.ATXID,
	nodeID types.NodeID,
	commitmentATX types.ATXID,
	marriageIndex *uint32,
) error {
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
	if marriageIndex != nil {
		if !p.MarriageIndexProof.Valid(p.SubPostRoot, *marriageIndex) {
			return errors.New("invalid marriage index proof")
		}
	}
	if !p.PostProof.Valid(p.SubPostRoot, p.Post.Root()) {
		return errors.New("invalid PoST proof")
	}
	if !p.NumUnitsProof.Valid(p.SubPostRoot, p.NumUnits) {
		return errors.New("invalid num units proof")
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
