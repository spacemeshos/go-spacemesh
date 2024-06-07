package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// ProofDoublePublish is a proof that two distinct ATXs with the same publish epoch were signed by the same smesher.
type ProofDoublePublish struct {
	Proofs [2]PublishProof
}

// Valid returns true if the proof is valid. It verifies that the two proofs have the same publish epoch, smesher ID,
// and a valid signature but different ATX IDs as well as that the provided merkle proofs are valid.
func (p ProofDoublePublish) Valid(edVerifier *signing.EdVerifier) (bool, error) {
	if p.Proofs[0].PubEpoch != p.Proofs[1].PubEpoch {
		return false, errors.New("proofs have different publish epochs")
	}

	if p.Proofs[0].SmesherID != p.Proofs[1].SmesherID {
		return false, errors.New("proofs have different smesher IDs")
	}

	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return false, errors.New("proofs have the same ATX ID")
	}

	atx1Valid, err := p.Proofs[0].Valid(edVerifier)
	if err != nil {
		return false, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if !atx1Valid {
		return false, errors.New("proof 1 is invalid")
	}

	atx2Valid, err := p.Proofs[1].Valid(edVerifier)
	if err != nil {
		return false, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return atx2Valid, nil
}

// PublishProof proofs that an ATX was published with a given publish epoch by a given smesher.
type PublishProof struct {
	// ATXID is the ID of the ATX being proven. It is the merkle root from the contents of the ATX.
	ATXID types.ATXID
	// PubEpoch is the epoch in which the ATX was published.
	PubEpoch types.EpochID
	// Proof contains the merkle path from the root of the ATX merkle tree (ATXID) to the PublishEpoch field.
	Proof []types.Hash32 `scale:"max=32"`

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

// Valid returns true if the proof is valid. It verifies that the signature is valid and that the merkle proof is valid.
func (p PublishProof) Valid(edVerifier *signing.EdVerifier) (bool, error) {
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return false, errors.New("invalid signature")
	}
	proof := make([][]byte, len(p.Proof))
	for i, h := range p.Proof {
		proof[i] = h.Bytes()
	}
	epoch := make([]byte, 4)
	binary.LittleEndian.PutUint32(epoch, p.PubEpoch.Uint32())
	return merkle.ValidatePartialTree(
		[]uint64{uint64(PublishEpochIndex)},
		[][]byte{epoch},
		proof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
}
