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

	// NodeID is the node ID that created the invalid proof
	NodeID types.NodeID

	// Marriage is the proof for the marriage ATX of the smesher. It proofs that NodeID agreed to marry the signer
	// of the ATX.
	MarriageProof MarryProof

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

		MarriageProof:    marriageProof,
		CommitmentProof:  commitmentProof,
		InvalidPostProof: invalidPostProof,
	}
	return proof, nil
}

func createCommitmentProof(atx *ActivationTxV2, nodeID types.NodeID) (CommitmentProof, error) {
	if atx.SmesherID != nodeID {
		return CommitmentProof{}, errors.New("node ID does not match smesher ID")
	}

	initialPostRootProof, err := initialPostRootProof(atx)
	if err != nil {
		return CommitmentProof{}, fmt.Errorf("failed to create initial PoST proof: %w", err)
	}

	commitmentATXProof, err := commitmentProof(atx)
	if err != nil {
		return CommitmentProof{}, fmt.Errorf("failed to create commitment ATX proof: %w", err)
	}

	proof := CommitmentProof{
		InitialATXID: atx.ID(),

		InitialPostRoot:  types.Hash32(atx.Initial.Root()),
		InitialPostProof: initialPostRootProof,

		CommitmentATX:      atx.Initial.CommitmentATX,
		CommitmentATXProof: commitmentATXProof,

		Signature: atx.Signature,
	}
	return proof, nil
}

func initialPostRootProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(InitialPostsRootIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func commitmentProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(CommitmentATXIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.Initial.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func createInvalidPostProof(atx *ActivationTxV2, nipostIndex, marriageIndex int) (InvalidPostProof, error) {
	marriageATXProof, err := marriageATXProof(atx)
	if err != nil {
		return InvalidPostProof{}, fmt.Errorf("failed to create marriage ATX proof: %w", err)
	}

	niPostsTreeProof, err := niPostsTreeProof(atx)
	if err != nil {
		return InvalidPostProof{}, fmt.Errorf("failed to create NiPoSTs tree proof: %w", err)
	}

	niPostsRootProof, err := niPostsRootProof(atx.NiPosts, nipostIndex, atx.PreviousATXs)
	if err != nil {
		return InvalidPostProof{}, fmt.Errorf("failed to create NiPoSTs root proof: %w", err)
	}

	nipost := atx.NiPosts[nipostIndex]
	challengeProof, err := challengeProof(nipost, atx.PreviousATXs)
	if err != nil {
		return InvalidPostProof{}, fmt.Errorf("failed to create challenge proof: %w", err)
	}

	postIndex := slices.IndexFunc(nipost.Posts, func(post SubPostV2) bool {
		return post.MarriageIndex == uint32(marriageIndex)
	})

	postsRootProof, err := postsRootProof(nipost, atx.PreviousATXs)
	if err != nil {
		return InvalidPostProof{}, fmt.Errorf("failed to create PoSTs root proof: %w", err)
	}

	subPostRootProof, err := subPostRootProof(nipost.Posts, postIndex, atx.PreviousATXs)
	if err != nil {
		return InvalidPostProof{}, fmt.Errorf("failed to create sub PoST root proof: %w", err)
	}

	proof := InvalidPostProof{
		MarriageATXProof: marriageATXProof,

		NiPostsTreeRoot:  types.Hash32(atx.NiPosts.Root(atx.PreviousATXs)),
		NiPostsTreeProof: niPostsTreeProof,

		NiPostsRoot:      types.Hash32(nipost.Root(atx.PreviousATXs)),
		NiPostRootIndex:  uint16(nipostIndex),
		NiPostsRootProof: niPostsRootProof,

		Challenge:      nipost.Challenge,
		ChallengeProof: challengeProof,

		PostsRoot:      types.Hash32(nipost.Posts.Root(atx.PreviousATXs)),
		PostsRootProof: postsRootProof,

		SubPostRoot:      types.Hash32(nipost.Posts[postIndex].Root(atx.PreviousATXs)),
		SubPostRootIndex: uint16(postIndex),
		SubPostRootProof: subPostRootProof,

		MarriageIndexProof: nipost.Posts[postIndex].MarriageIndexProof(atx.PreviousATXs),

		// TODO(mafa): continue with proof
	}
	return proof, nil
}

func niPostsTreeProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(NIPostsRootIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func niPostsRootProof(niposts NiPosts, index int, prevATXs []types.ATXID) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(index): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	niposts.merkleTree(tree, prevATXs)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func challengeProof(nipost NiPostsV2, prevATXs []types.ATXID) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(ChallengeIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	nipost.merkleTree(tree, prevATXs)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func marriageATXProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(MarriageATXIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func postsRootProof(nipost NiPostsV2, prevATXs []types.ATXID) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(PostsRootIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	nipost.merkleTree(tree, prevATXs)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func subPostRootProof(posts SubPostsV2, postIndex int, prevATXs []types.ATXID) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(postIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	posts.merkleTree(tree, prevATXs)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

// Valid returns true if the proof is valid. It verifies that the two proofs have the same publish epoch, smesher ID,
// and a valid signature but different ATX IDs as well as that the provided merkle proofs are valid.
func (p ProofMergedInvalidPost) Valid(ctx context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if !malValidator.Signature(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return types.EmptyNodeID, errors.New("invalid signature")
	}

	if err := p.MarriageProof.Valid(malValidator, p.NodeID); err != nil {
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
		p.MarriageProof.ATXID,
		p.MarriageProof.CertificateIndex,
	); err != nil {
		return types.EmptyNodeID, fmt.Errorf("invalid invalid post proof: %w", err)
	}

	return p.NodeID, nil
}

// CommitmentProof is a proof for the commitment ATX of a smesher. It is generated from the initial ATX of the smesher.
type CommitmentProof struct {
	// InitialATXID is the ID of the initial ATX of the smesher.
	InitialATXID types.ATXID

	// InitialPostRoot is the root of the initial PoST merkle tree.
	InitialPostRoot types.Hash32
	// InitialPostProof contains the merkle path from the root of the merkle tree to the root of the InitialPost.
	InitialPostProof []types.Hash32 `scale:"max=32"`

	// CommitmentATX is the ATX that was used by the identity as their commitment ATX.
	CommitmentATX types.ATXID
	// CommitmentATXProof contains the merkle path from the root of the merkle tree to the CommitmentATX
	// field.
	CommitmentATXProof []types.Hash32 `scale:"max=32"`

	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

// Valid returns no error if the proof is valid. It verifies that the signature is valid and that the merkle proofs
// are valid.
func (p CommitmentProof) Valid(malValidator MalfeasanceValidator, nodeID types.NodeID) error {
	if !malValidator.Signature(signing.ATX, nodeID, p.InitialATXID.Bytes(), p.Signature) {
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
		p.InitialATXID.Bytes(),
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

// InvalidPostProof is a proof for an invalid PoST in an ATX. It contains the PoST and the merkle proofs to verify the
// PoST.
type InvalidPostProof struct {
	// --- MarriageATX ---

	// MarriageATXProof contains the merkle path from the NiPostsRoot to the MarriageATX field.
	MarriageATXProof []types.Hash32 `scale:"max=32"`

	// --- NiPost ---

	// NiPostsTreeRoot is the root of the merkle tree containing the NiPoSTs of the ATX.
	NiPostsTreeRoot types.Hash32
	// NiPostsTreeProof contains the merkle path from the root of the ATX merkle tree (ATXID) to the Post field.
	NiPostsTreeProof []types.Hash32 `scale:"max=32"`

	// NiPostsRoot is the root of the NiPoST containing the invalid PoST.
	NiPostsRoot types.Hash32
	// NiPostsRootIndex is the index of the NiPoST in the NiPoSTs tree.
	NiPostRootIndex uint16
	// NiPostsRootProof contains the merkle path from the NiPostsTreeRoot to the NiPostRoot field.
	NiPostsRootProof []types.Hash32 `scale:"max=32"`

	// --- Challenge for PoST ---

	// Challenge for the NiPoST.
	Challenge types.Hash32
	// ChallengeProof contains the merkle path from the NiPostsRoot to the Challenge field.
	ChallengeProof []types.Hash32 `scale:"max=32"`

	// --- PoST ---

	// PostsRoot is the root of the PoST merkle tree.
	PostsRoot types.Hash32
	// PostsRootProof contains the merkle path from the NiPostsRoot to the PostsRoot field.
	PostsRootProof []types.Hash32 `scale:"max=32"`

	// SubPostRoot is the root of the sub PoST merkle tree.
	SubPostRoot types.Hash32
	// SubPostRootIndex is the index of the sub PoST in the NiPoST.
	SubPostRootIndex uint16
	// SubPostRootProof contains the merkle path from the PostsRoot to the SubPostRoot field.
	SubPostRootProof []types.Hash32 `scale:"max=32"`

	// MarriageIndexProof contains the merkle path from the SubPostRoot to the MarriageIndex field.
	MarriageIndexProof []types.Hash32 `scale:"max=32"`
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
	marriageIndex uint64,
) error {
	// --- MarriageATX ---

	marriageProof := make([][]byte, len(p.MarriageATXProof))
	for i, h := range p.MarriageATXProof {
		marriageProof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(MarriageATXIndex)},
		[][]byte{marriageATX.Bytes()},
		marriageProof,
		atxID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate marriage ATX proof: %w", err)
	}
	if !ok {
		return errors.New("invalid marriage ATX proof")
	}

	// --- NiPoST ---

	nipostsTreeProof := make([][]byte, len(p.NiPostsTreeProof))
	for i, h := range p.NiPostsTreeProof {
		nipostsTreeProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(NIPostsRootIndex)},
		[][]byte{p.NiPostsTreeRoot.Bytes()},
		nipostsTreeProof,
		atxID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate NiPoST root proof: %w", err)
	}
	if !ok {
		return errors.New("invalid NiPoST root proof")
	}

	nipostsProof := make([][]byte, len(p.NiPostsRootProof))
	for i, h := range p.NiPostsRootProof {
		nipostsProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(p.NiPostRootIndex)},
		[][]byte{p.NiPostsRoot.Bytes()},
		nipostsProof,
		p.NiPostsTreeRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate NiPoST proof: %w", err)
	}
	if !ok {
		return errors.New("invalid NiPoST proof")
	}

	// --- Challenge for PoST ---

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

	// --- PoST ---

	postsProof := make([][]byte, len(p.PostsRootProof))
	for i, h := range p.PostsRootProof {
		postsProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{uint64(PostsRootIndex)},
		[][]byte{p.PostsRoot.Bytes()},
		postsProof,
		p.NiPostsTreeRoot.Bytes(),
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
		[]uint64{uint64(p.SubPostRootIndex)},
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

	marriageIndexProof := make([][]byte, len(p.MarriageIndexProof))
	for i, h := range p.MarriageIndexProof {
		marriageIndexProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{marriageIndex},
		[][]byte{p.Post.Root()},
		marriageIndexProof,
		p.SubPostRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate PoST marriage index proof: %w", err)
	}
	if !ok {
		return errors.New("invalid PoST marriage index proof")
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
