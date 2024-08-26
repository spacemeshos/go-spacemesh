package wire

import (
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

//go:generate scalegen

// ProofInvalidPrevAtxV2 is a proof that two distinct ATXs reference the same previous ATX for one of the included
// identities.
//
// We are proving the following:
// 1. The ATXs have different IDs.
// 2. Both ATXs have a valid signature.
// 3. Both ATXs reference the same previous ATX for the same identity.
// 4. Both marriage certificates have valid signatures.
//
// HINT: this works if the identity that publishes the marriage ATX marries themselves.
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

	if atx1.MarriageATX == nil && atx1.SmesherID != nodeID {
		return nil, fmt.Errorf("ATX1 was not created by %s", nodeID)
	}
	equivocationSet1 := []types.NodeID{atx1.SmesherID}
	if atx1.MarriageATX != nil {
		id, err := marriage.FindIDByNodeID(db, atx1.SmesherID)
		if err != nil {
			return nil, fmt.Errorf("find ID by node ID: %w", err)
		}
		equivocationSet1, err = marriage.NodeIDsByID(db, id)
		if err != nil {
			return nil, fmt.Errorf("get equivocation set for ATX1: %w", err)
		}
	}
	marriageIndex1 := slices.Index(equivocationSet1, nodeID)
	if marriageIndex1 == -1 {
		return nil, fmt.Errorf("%s did not contribute to ATX1", nodeID)
	}

	proof1, err := createInvalidPrevAtxProof(atx1, uint32(marriageIndex1))
	if err != nil {
		return nil, fmt.Errorf("proof for atx1: %w", err)
	}

	if atx2.MarriageATX == nil && atx2.SmesherID != nodeID {
		return nil, fmt.Errorf("ATX2 was not created by %s", nodeID)
	}
	equivocationSet2 := []types.NodeID{atx2.SmesherID}
	if atx2.MarriageATX != nil {
		id, err := marriage.FindIDByNodeID(db, atx1.SmesherID)
		if err != nil {
			return nil, fmt.Errorf("find ID by node ID: %w", err)
		}
		equivocationSet2, err = marriage.NodeIDsByID(db, id)
		if err != nil {
			return nil, fmt.Errorf("get equivocation set for ATX2: %w", err)
		}
	}
	marriageIndex2 := slices.Index(equivocationSet2, nodeID)
	if marriageIndex2 == -1 {
		return nil, fmt.Errorf("%s did not contribute to ATX2", nodeID)
	}

	proof2, err := createInvalidPrevAtxProof(atx2, uint32(marriageIndex2))
	if err != nil {
		return nil, fmt.Errorf("proof for atx2: %w", err)
	}

	proof := &ProofInvalidPrevAtxV2{
		NodeID:  nodeID,
		PrevATX: types.EmptyATXID,
		Proofs:  [2]InvalidPrevAtxProof{proof1, proof2},
	}
	return proof, nil
}

func createInvalidPrevAtxProof(atx *ActivationTxV2, marriageIndex uint32) (InvalidPrevAtxProof, error) {
	prevATXsRootProof, err := prevRootProof(atx)
	if err != nil {
		return InvalidPrevAtxProof{}, fmt.Errorf("create proof for previous atx root: %w", err)
	}

	prevATXProof, err := prevProof(atx.PreviousATXs, marriageIndex)
	if err != nil {
		return InvalidPrevAtxProof{}, fmt.Errorf("create proof for previous atx: %w", err)
	}

	proof := InvalidPrevAtxProof{
		ATXID: atx.ID(),

		PreviousATXsRoot:      types.Hash32(atx.PreviousATXs.Root()),
		PreviousATXsRootProof: prevATXsRootProof,

		PrevATXIndex: marriageIndex,
		PrevATXProof: prevATXProof,

		SmesherID: atx.SmesherID,
		Signature: atx.Signature,
	}

	return proof, nil
}

func prevRootProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(PreviousATXsRootIndex): true}).
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

func prevProof(prevATXs PrevATXs, prevAtxIndex uint32) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(prevAtxIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	prevATXs.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func (p ProofInvalidPrevAtxV2) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("proofs have the same ATX ID")
	}

	if err := p.Proofs[0].Valid(edVerifier); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proofs[1].Valid(edVerifier); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return types.EmptyNodeID, errors.New("not implemented")
}

// ProofInvalidPrevAtxV1 is a proof that two ATXs published by an identity reference the same previous ATX for an
// identity.
//
// We are proving the following:
// 1. The ATXs have different IDs.
// 2. Both ATXs have a valid signature.
// 3. Both ATXs contain a marriage certificate created by the same identity.
// 4. Both marriage certificates have valid signatures.
//
// HINT: this works if the identity that publishes the marriage ATX marries themselves.
type ProofInvalidPrevAtxV1 struct {
	// NodeID is the node ID that referenced the same previous ATX twice.
	NodeID types.NodeID

	Proofs [2]InvalidPrevAtxProof
}

var _ Proof = &ProofInvalidPrevAtxV2{}

func NewInvalidPrevAtxProofV1(
	atx1 *ActivationTxV2,
	atx2 *ActivationTxV1,
	nodeID types.NodeID,
) (*ProofInvalidPrevAtxV1, error) {
	return nil, errors.New("not implemented")
}

func (p ProofInvalidPrevAtxV1) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	return types.EmptyNodeID, errors.New("not implemented")
}

type InvalidPrevAtxProof struct {
	// ATXID is the ID of the ATX being proven.
	ATXID types.ATXID

	// PreviousATXsRoot and its proof that it is contained in the ATX.
	PreviousATXsRoot      types.Hash32
	PreviousATXsRootProof []types.Hash32 `scale:"max=32"`

	// PrevATXProof is the proof that the PrevATX in question is contained in the ATX at the given index.
	PrevATXIndex uint32
	PrevATXProof []types.Hash32 `scale:"max=32"`

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

func (p InvalidPrevAtxProof) Valid(edVerifier *signing.EdVerifier) error {
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid ATX signature")
	}

	return errors.New("not implemented")
}
