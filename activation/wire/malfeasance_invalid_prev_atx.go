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

// ProofInvalidPrevAtxV2 is a proof that two distinct ATXs reference the same previous ATX for one of the included
// identities.
//
// We are proving the following:
//  1. The ATXs have different IDs.
//  2. Both ATXs have a valid signature.
//  3. Both ATXs reference the same previous ATX for the same identity.
//  4. If the signer of one of the two ATXs is not the identity that referenced the same previous ATX, then the identity
//     that did is married to the signer via a valid marriage certificate in the referenced marriage ATX.
type ProofInvalidPrevAtxV2 struct {
	// NodeID is the node ID that referenced the same previous ATX twice.
	NodeID types.NodeID

	// PrevATX is the ATX that was referenced twice.
	PrevATX types.ATXID

	Proofs [2]InvalidPrevAtxProof
}

var _ Proof = &ProofInvalidPrevAtxV2{}

func NewInvalidPrevAtxProofV2(
	db sql.Executor,
	atx1, atx2 *ActivationTxV2,
	nodeID types.NodeID,
) (*ProofInvalidPrevAtxV2, error) {
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}

	if atx1.SmesherID != nodeID && atx1.MarriageATX == nil {
		return nil, errors.New("ATX1 is not a merged ATX, but NodeID is different from SmesherID")
	}

	if atx2.SmesherID != nodeID && atx2.MarriageATX == nil {
		return nil, errors.New("ATX2 is not a merged ATX, but NodeID is different from SmesherID")
	}

	var marriageProof1 *MarriageProof
	nipostIndex1 := 0
	postIndex1 := 0
	if atx1.SmesherID != nodeID {
		proof, err := createMarriageProof(db, atx1, nodeID)
		if err != nil {
			return nil, fmt.Errorf("marriage proof: %w", err)
		}
		marriageProof1 = &proof
		for i, nipost := range atx1.NIPosts {
			postIndex1 = slices.IndexFunc(nipost.Posts, func(post SubPostV2) bool {
				return post.MarriageIndex == proof.NodeIDMarryProof.CertificateIndex
			})
			if postIndex1 != -1 {
				nipostIndex1 = i
				break
			}
		}
		if postIndex1 == -1 {
			return nil, fmt.Errorf("no PoST from %s in ATX", nodeID.ShortString())
		}
	}

	var marriageProof2 *MarriageProof
	nipostIndex2 := 0
	postIndex2 := 0
	if atx2.SmesherID != nodeID {
		proof, err := createMarriageProof(db, atx2, nodeID)
		if err != nil {
			return nil, fmt.Errorf("marriage proof: %w", err)
		}
		marriageProof2 = &proof
		for i, nipost := range atx2.NIPosts {
			postIndex2 = slices.IndexFunc(nipost.Posts, func(post SubPostV2) bool {
				return post.MarriageIndex == proof.NodeIDMarryProof.CertificateIndex
			})
			if postIndex2 != -1 {
				nipostIndex2 = i
				break
			}
		}
		if postIndex2 == -1 {
			return nil, fmt.Errorf("no PoST from %s in ATX", nodeID.ShortString())
		}
	}

	prevATX1 := atx1.PreviousATXs[atx1.NIPosts[nipostIndex1].Posts[postIndex1].PrevATXIndex]
	prevATX2 := atx2.PreviousATXs[atx2.NIPosts[nipostIndex2].Posts[postIndex2].PrevATXIndex]
	if prevATX1 != prevATX2 {
		return nil, errors.New("ATXs reference different previous ATXs")
	}

	proof1, err := createInvalidPrevAtxProof(atx1, prevATX1, nipostIndex1, postIndex1, marriageProof1)
	if err != nil {
		return nil, fmt.Errorf("proof for atx1: %w", err)
	}

	proof2, err := createInvalidPrevAtxProof(atx2, prevATX2, nipostIndex2, postIndex2, marriageProof2)
	if err != nil {
		return nil, fmt.Errorf("proof for atx2: %w", err)
	}

	proof := &ProofInvalidPrevAtxV2{
		NodeID:  nodeID,
		PrevATX: prevATX1,
		Proofs:  [2]InvalidPrevAtxProof{proof1, proof2},
	}
	return proof, nil
}

func createInvalidPrevAtxProof(
	atx *ActivationTxV2,
	prevATX types.ATXID,
	nipostIndex,
	postIndex int,
	marriageProof *MarriageProof,
) (InvalidPrevAtxProof, error) {
	proof := InvalidPrevAtxProof{
		ATXID: atx.ID(),

		NIPostsRoot:      atx.NIPosts.Root(atx.PreviousATXs),
		NIPostsRootProof: atx.NIPostsRootProof(),

		NIPostRoot:      atx.NIPosts[nipostIndex].Root(atx.PreviousATXs),
		NIPostRootProof: atx.NIPosts.Proof(int(nipostIndex), atx.PreviousATXs),
		NIPostIndex:     uint16(nipostIndex),

		SubPostsRoot:      atx.NIPosts[nipostIndex].Posts.Root(atx.PreviousATXs),
		SubPostsRootProof: atx.NIPosts[nipostIndex].PostsRootProof(atx.PreviousATXs),

		SubPostRoot:      atx.NIPosts[nipostIndex].Posts[postIndex].Root(atx.PreviousATXs),
		SubPostRootProof: atx.NIPosts[nipostIndex].Posts.Proof(postIndex, atx.PreviousATXs),
		SubPostRootIndex: uint16(postIndex),

		MarriageIndexProof: atx.NIPosts[nipostIndex].Posts[postIndex].MarriageIndexProof(atx.PreviousATXs),
		MarriageProof:      marriageProof,

		PrevATXProof: atx.NIPosts[nipostIndex].Posts[postIndex].PrevATXProof(prevATX),

		SmesherID: atx.SmesherID,
		Signature: atx.Signature,
	}

	return proof, nil
}

func (p ProofInvalidPrevAtxV2) Valid(_ context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("proofs have the same ATX ID")
	}
	if err := p.Proofs[0].Valid(p.PrevATX, p.NodeID, malValidator); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proofs[1].Valid(p.PrevATX, p.NodeID, malValidator); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return p.NodeID, nil
}

// ProofInvalidPrevAtxV1 is a proof that two ATXs published by an identity reference the same previous ATX for an
// identity.
//
// We are proving the following:
//  1. Both ATXs have a valid signature.
//  2. Both ATXs reference the same previous ATX for the same identity.
//  3. If the signer of the ATXv2 is not the identity that referenced the same previous ATX, then the included marriage
//     proof is valid.
//  4. The ATXv1 has been signed by the identity that referenced the same previous ATX.
type ProofInvalidPrevAtxV1 struct {
	// NodeID is the node ID that referenced the same previous ATX twice.
	NodeID types.NodeID

	// PrevATX is the ATX that was referenced twice.
	PrevATX types.ATXID

	Proof InvalidPrevAtxProof
	ATXv1 ActivationTxV1
}

var _ Proof = &ProofInvalidPrevAtxV1{}

func NewInvalidPrevAtxProofV1(
	db sql.Executor,
	atx1 *ActivationTxV2,
	atx2 *ActivationTxV1,
	nodeID types.NodeID,
) (*ProofInvalidPrevAtxV1, error) {
	if atx1.SmesherID != nodeID && atx1.MarriageATX == nil {
		return nil, errors.New("ATX1 is not a merged ATX, but NodeID is different from SmesherID")
	}

	if atx2.SmesherID != nodeID {
		return nil, errors.New("ATX2 is not signed by NodeID")
	}

	var marriageProof *MarriageProof
	nipostIndex := 0
	postIndex := 0
	if atx1.SmesherID != nodeID {
		proof, err := createMarriageProof(db, atx1, nodeID)
		if err != nil {
			return nil, fmt.Errorf("marriage proof: %w", err)
		}
		marriageProof = &proof
		for i, nipost := range atx1.NIPosts {
			postIndex = slices.IndexFunc(nipost.Posts, func(post SubPostV2) bool {
				return post.MarriageIndex == proof.NodeIDMarryProof.CertificateIndex
			})
			if postIndex != -1 {
				nipostIndex = i
				break
			}
		}
		if postIndex == -1 {
			return nil, fmt.Errorf("no PoST from %s in ATX", nodeID.ShortString())
		}
	}
	prevATX1 := atx1.PreviousATXs[atx1.NIPosts[nipostIndex].Posts[postIndex].PrevATXIndex]
	prevATX2 := atx2.PrevATXID
	if prevATX1 != prevATX2 {
		return nil, errors.New("ATXs reference different previous ATXs")
	}

	proof, err := createInvalidPrevAtxProof(atx1, prevATX1, nipostIndex, postIndex, marriageProof)
	if err != nil {
		return nil, fmt.Errorf("proof for atx1: %w", err)
	}

	return &ProofInvalidPrevAtxV1{
		NodeID:  nodeID,
		PrevATX: prevATX1,
		Proof:   proof,
		ATXv1:   *atx2,
	}, nil
}

func (p ProofInvalidPrevAtxV1) Valid(_ context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if err := p.Proof.Valid(p.PrevATX, p.NodeID, malValidator); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof is invalid: %w", err)
	}
	if !malValidator.Signature(signing.ATX, p.ATXv1.SmesherID, p.ATXv1.SignedBytes(), p.ATXv1.Signature) {
		return types.EmptyNodeID, errors.New("invalid ATX signature")
	}
	if p.NodeID != p.ATXv1.SmesherID {
		return types.EmptyNodeID, errors.New("ATXv1 has not been signed by the same identity")
	}
	if p.ATXv1.PrevATXID != p.PrevATX {
		return types.EmptyNodeID, errors.New("ATXv1 references a different previous ATX")
	}
	return p.NodeID, nil
}

type InvalidPrevAtxProof struct {
	// ATXID is the ID of the ATX being proven.
	ATXID types.ATXID
	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature

	// NIPostsRoot and its proof that it is contained in the ATX.
	NIPostsRoot      NIPostsRoot
	NIPostsRootProof NIPostsRootProof `scale:"max=32"`

	// NIPostRoot and its proof that it is contained at the given index in the NIPostsRoot.
	NIPostRoot      NIPostRoot
	NIPostRootProof NIPostRootProof `scale:"max=32"`
	NIPostIndex     uint16

	// SubPostsRoot and its proof that it is contained in the NIPostRoot.
	SubPostsRoot      SubPostsRoot
	SubPostsRootProof SubPostsRootProof `scale:"max=32"`

	// SubPostRoot and its proof that is contained at the given index in the SubPostsRoot.
	SubPostRoot      SubPostRoot
	SubPostRootProof SubPostRootProof `scale:"max=32"`
	SubPostRootIndex uint16

	// MarriageProof is the proof that NodeID and SmesherID are married. It is nil if NodeID == SmesherID.
	MarriageProof *MarriageProof

	// MarriageIndexProof is the proof that the MarriageIndex (CertificateIndex from NodeIDMarryProof) is contained in
	// the SubPostRoot.
	MarriageIndexProof MarriageIndexProof `scale:"max=32"`

	// PrevATXProof is the proof that the previous ATX is contained in the SubPostRoot.
	PrevATXProof PrevATXProof `scale:"max=32"`
}

func (p InvalidPrevAtxProof) Valid(prevATX types.ATXID, nodeID types.NodeID, malValidator MalfeasanceValidator) error {
	if !malValidator.Signature(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid ATX signature")
	}

	if nodeID != p.SmesherID && p.MarriageProof == nil {
		return errors.New("missing marriage proof")
	}

	if !p.NIPostsRootProof.Valid(p.ATXID, p.NIPostsRoot) {
		return errors.New("invalid NIPosts root proof")
	}
	if !p.NIPostRootProof.Valid(p.NIPostsRoot, int(p.NIPostIndex), p.NIPostRoot) {
		return errors.New("invalid NIPoST root proof")
	}
	if !p.SubPostsRootProof.Valid(p.NIPostRoot, p.SubPostsRoot) {
		return errors.New("invalid sub PoSTs root proof")
	}
	if !p.SubPostRootProof.Valid(p.SubPostsRoot, int(p.SubPostRootIndex), p.SubPostRoot) {
		return errors.New("invalid sub PoST root proof")
	}

	var marriageIndex *uint32
	if p.MarriageProof != nil {
		if err := p.MarriageProof.Valid(malValidator, p.ATXID, nodeID, p.SmesherID); err != nil {
			return fmt.Errorf("invalid marriage proof: %w", err)
		}
		marriageIndex = &p.MarriageProof.NodeIDMarryProof.CertificateIndex
	}
	if marriageIndex != nil {
		if !p.MarriageIndexProof.Valid(p.SubPostRoot, *marriageIndex) {
			return errors.New("invalid marriage index proof")
		}
	}

	if !p.PrevATXProof.Valid(p.SubPostRoot, prevATX) {
		return errors.New("invalid previous ATX proof")
	}

	return nil
}
