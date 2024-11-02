package wire

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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
			return nil, errors.New("marriage index not found in PoSTs of ATX")
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

	proof := &ProofInvalidPost{
		ATXID:     atx.ID(),
		SmesherID: atx.SmesherID,
		Signature: atx.Signature,

		NodeID: nodeID,

		MarriageProof: marriageProof,

		CommitmentProof:  commitmentProof,
		InvalidPostProof: invalidPostProof,
	}
	return proof, nil
}

func (p ProofInvalidPost) Valid(ctx context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if !malValidator.Signature(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return types.EmptyNodeID, errors.New("invalid signature")
	}

	if err := p.MarriageProof.Valid(malValidator, p.ATXID, p.NodeID, p.SmesherID); err != nil {
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
		p.MarriageProof.NodeIDMarryProof.CertificateIndex,
	); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid invalid post proof: %w", err)
	}

	return p.NodeID, nil
}

// MarriageProof is a proof for two identities to be married via a marriage ATX.
type MarriageProof struct {
	// MarriageATX and its proof that it is contained in the ATX.
	MarriageATX      types.ATXID
	MarriageATXProof MarriageATXProof `scale:"max=32"`
	// MarriageATXSmesherID is the ID of the smesher that published the marriage ATX.
	MarriageATXSmesherID types.NodeID

	// NodeIDMarryProof is the proof that NodeID married in MarriageATX.
	NodeIDMarryProof MarryProof
	// SmesherIDMarryProof is the proof that SmesherID married in MarriageATX.
	SmesherIDMarryProof MarryProof
}

func createMarriageProof(db sql.Executor, atx *ActivationTxV2, nodeID types.NodeID) (MarriageProof, error) {
	if nodeID == atx.SmesherID {
		// we don't need a marriage proof if the node ID is the same as the smesher ID
		return MarriageProof{}, errors.New("node ID is the same as smesher ID")
	}

	var blob sql.Blob
	v, err := atxs.LoadBlob(context.Background(), db, atx.MarriageATX.Bytes(), &blob)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("get marriage ATX: %w", err)
	}
	if v != types.AtxV2 {
		return MarriageProof{}, errors.New("invalid ATX version for marriage ATX")
	}
	marriageATX, err := DecodeAtxV2(blob.Bytes)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("decode marriage ATX: %w", err)
	}

	nodeIDmarriageProof, err := createMarryProof(db, marriageATX, nodeID)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("NodeID marriage proof: %w", err)
	}

	smesherIDmarriageProof, err := createMarryProof(db, marriageATX, atx.SmesherID)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("SmesherID marriage proof: %w", err)
	}

	proof := MarriageProof{
		MarriageATX:      marriageATX.ID(),
		MarriageATXProof: atx.MarriageATXProof(),

		MarriageATXSmesherID: marriageATX.SmesherID,

		NodeIDMarryProof:    nodeIDmarriageProof,
		SmesherIDMarryProof: smesherIDmarriageProof,
	}
	return proof, nil
}

func (p MarriageProof) Valid(
	malValidator MalfeasanceValidator,
	atxID types.ATXID,
	nodeID,
	smesherID types.NodeID,
) error {
	if !p.MarriageATXProof.Valid(atxID, p.MarriageATX) {
		return errors.New("invalid marriage ATX proof")
	}

	if err := p.NodeIDMarryProof.Valid(malValidator, p.MarriageATX, p.MarriageATXSmesherID, nodeID); err != nil {
		return fmt.Errorf("invalid marriage proof for NodeID: %w", err)
	}

	if err := p.SmesherIDMarryProof.Valid(malValidator, p.MarriageATX, p.MarriageATXSmesherID, smesherID); err != nil {
		return fmt.Errorf("invalid marriage proof for SmesherID: %w", err)
	}

	return nil
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
	MarriageIndexProof MarriageIndexProof `scale:"max=32"` // TODO(mafa): include this with marriage ATX proof

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

	proof := InvalidPostProof{
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
	marriageIndex uint32,
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

	if !p.MarriageIndexProof.Valid(p.SubPostRoot, marriageIndex) {
		return errors.New("invalid marriage index proof")
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
