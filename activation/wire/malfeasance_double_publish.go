package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

// ProofDoublePublish is a proof that two distinct ATXs with the same publish epoch were signed by the same smesher.
//
// We are proofing the following:
// 1. The ATXs have different IDs.
// 2. Both ATXs have a valid signature.
// 3. Both ATXs were signed by the same smesher.
// 4. Both ATXs have the same publish epoch.
type ProofDoublePublish struct {
	Proofs [2]PublishProof
}

var _ Proof = &ProofDoublePublish{}

func NewDoublePublishProof(atx1, atx2 *ActivationTxV2) (*ProofDoublePublish, error) {
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}

	if atx1.SmesherID != atx2.SmesherID {
		return nil, errors.New("ATXs have different smesher IDs")
	}

	if atx1.PublishEpoch != atx2.PublishEpoch {
		return nil, errors.New("ATXs have different publish epochs")
	}

	proof1, err := publishEpochProof(atx1)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof for ATX 1: %w", err)
	}

	proof2, err := publishEpochProof(atx2)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof for ATX 2: %w", err)
	}

	proof := &ProofDoublePublish{
		Proofs: [2]PublishProof{
			{
				ATXID:     atx1.ID(),
				PubEpoch:  atx1.PublishEpoch,
				Proof:     proof1,
				SmesherID: atx1.SmesherID,
				Signature: atx1.Signature,
			},
			{
				ATXID:     atx2.ID(),
				PubEpoch:  atx2.PublishEpoch,
				Proof:     proof2,
				SmesherID: atx2.SmesherID,
				Signature: atx2.Signature,
			},
		},
	}

	return proof, nil
}

func publishEpochProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(PublishEpochIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, h := range proof {
		proofHashes[i] = types.Hash32(h)
	}
	return proofHashes, nil
}

// Valid returns true if the proof is valid. It verifies that the two proofs have the same publish epoch, smesher ID,
// and a valid signature but different ATX IDs as well as that the provided merkle proofs are valid.
func (p ProofDoublePublish) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("proofs have the same ATX ID")
	}
	if p.Proofs[0].SmesherID != p.Proofs[1].SmesherID {
		return types.EmptyNodeID, errors.New("proofs have different smesher IDs")
	}
	if p.Proofs[0].PubEpoch != p.Proofs[1].PubEpoch {
		return types.EmptyNodeID, errors.New("proofs have different publish epochs")
	}

	if err := p.Proofs[0].Valid(edVerifier); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proofs[1].Valid(edVerifier); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return p.Proofs[0].SmesherID, nil
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
func (p PublishProof) Valid(edVerifier *signing.EdVerifier) error {
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid signature")
	}
	proof := make([][]byte, len(p.Proof))
	for i, h := range p.Proof {
		proof[i] = h.Bytes()
	}
	epoch := make([]byte, 32)
	binary.LittleEndian.PutUint32(epoch, p.PubEpoch.Uint32())
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(PublishEpochIndex)},
		[][]byte{epoch},
		proof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate publish epoch proof: %w", err)
	}
	if !ok {
		return errors.New("invalid publish epoch proof")
	}
	return nil
}
