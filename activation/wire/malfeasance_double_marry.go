package wire

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate scalegen

// ProofDoubleMarry is a proof that two distinct ATXs contain a marriage certificate signed by the same identity.
//
// We are proving the following:
//  1. The ATXs have different IDs.
//  2. Both ATXs have a valid signature.
//  3. Both ATXs contain a marriage certificate created by the same identity.
//  4. Both marriage certificates have valid signatures.
type ProofDoubleMarry struct {
	// NodeID is the node ID that married twice.
	NodeID types.NodeID

	// ATX1 is the ID of the ATX being proven to have the marriage certificate of interest.
	ATX1 types.ATXID
	// SmesherID1 is the ID of the smesher that published ATX1.
	SmesherID1 types.NodeID
	// Signature1 is the signature of the ATXID by the smesher.
	Signature1 types.EdSignature
	// Proof1 is the proof that the marriage certificate is contained in the ATX1.
	Proof1 MarryProof

	// ATX2 is the ID of the ATX being proven to have the marriage certificate of interest.
	ATX2 types.ATXID
	// SmesherID2 is the ID of the smesher that published ATX2.
	SmesherID2 types.NodeID
	// Signature2 is the signature of the ATXID by the smesher.
	Signature2 types.EdSignature
	// Proof2 is the proof that the marriage certificate is contained in the ATX2.
	Proof2 MarryProof
}

var _ Proof = &ProofDoubleMarry{}

func NewDoubleMarryProof(db sql.Executor, atx1, atx2 *ActivationTxV2, nodeID types.NodeID) (*ProofDoubleMarry, error) {
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}

	proof1, err := createMarryProof(db, atx1, nodeID)
	if err != nil {
		return nil, fmt.Errorf("proof for atx1: %w", err)
	}
	proof2, err := createMarryProof(db, atx2, nodeID)
	if err != nil {
		return nil, fmt.Errorf("proof for atx2: %w", err)
	}

	return &ProofDoubleMarry{
		NodeID: nodeID,

		ATX1:       atx1.ID(),
		SmesherID1: atx1.SmesherID,
		Signature1: atx1.Signature,
		Proof1:     proof1,

		ATX2:       atx2.ID(),
		SmesherID2: atx2.SmesherID,
		Signature2: atx2.Signature,
		Proof2:     proof2,
	}, nil
}

func (p ProofDoubleMarry) Valid(_ context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if p.ATX1 == p.ATX2 {
		return types.EmptyNodeID, errors.New("proofs have the same ATX ID")
	}
	if !malValidator.Signature(signing.ATX, p.SmesherID1, p.ATX1.Bytes(), p.Signature1) {
		return types.EmptyNodeID, errors.New("invalid signature for ATX1")
	}
	if !malValidator.Signature(signing.ATX, p.SmesherID2, p.ATX2.Bytes(), p.Signature2) {
		return types.EmptyNodeID, errors.New("invalid signature for ATX2")
	}
	if err := p.Proof1.Valid(malValidator, p.ATX1, p.SmesherID1, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proof2.Valid(malValidator, p.ATX2, p.SmesherID2, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return p.NodeID, nil
}
