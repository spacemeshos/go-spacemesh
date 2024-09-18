package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate scalegen

// ProofDoubleMerge is a proof that two distinct ATXs published in the same epoch
// contain the same marriage ATX.
//
// We are proving the following:
//  1. The ATXs have different IDs.
//  2. Both ATXs have a valid signature.
//  3. Both ATXs contain the same marriage ATX.
//  4. Both ATXs were published in the same epoch.
//  5. Signers of both ATXs are married - to prevent banning others by
//     publishing an ATX with the same marriage ATX.
type ProofDoubleMerge struct {
	PublishEpoch  types.EpochID
	Proofs        [2]MergeProof
	MarriageProof MarriageProof
}

var _ Proof = &ProofDoubleMerge{}

// Valid implements Proof.Valid.
func (p *ProofDoubleMerge) Valid(verifier *signing.EdVerifier) (types.NodeID, error) {
	// 1. The ATXs have different IDs.
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("ATXs have the same ID")
	}

	// 2. Both ATXs have a valid signature.
	if !verifier.Verify(signing.ATX, p.Proofs[0].SmesherID, p.Proofs[0].ATXID.Bytes(), p.Proofs[0].Signature) {
		return types.EmptyNodeID, errors.New("ATX 1 invalid signature")
	}

	if !verifier.Verify(signing.ATX, p.Proofs[1].SmesherID, p.Proofs[1].ATXID.Bytes(), p.Proofs[1].Signature) {
		return types.EmptyNodeID, errors.New("ATX 2 invalid signature")
	}

	// 3. and 4. (publish epoch and marriage ATX)
	valid, err := p.Proofs[0].valid(verifier, p.PublishEpoch, p.MarriageProof.ATXID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating ATX 1 merge proof: %w", err)
	}
	if !valid {
		return types.EmptyNodeID, errors.New("ATX 1 merge proof is invalid")
	}

	valid, err = p.Proofs[1].valid(verifier, p.PublishEpoch, p.MarriageProof.ATXID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating ATX 2 merge proof: %w", err)
	}
	if !valid {
		return types.EmptyNodeID, errors.New("ATX 2 merge proof is invalid")
	}

	// 5. signers are married
	valid, err = p.MarriageProof.valid()
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating marriage proof: %w", err)
	}
	if !valid {
		return types.EmptyNodeID, errors.New("marriage proof is invalid")
	}

	return p.Proofs[0].SmesherID, nil
}

func NewDoubleMergeProof(db sql.Executor, atx1, atx2 *ActivationTxV2) (*ProofDoubleMerge, error) {
	if atx1.PublishEpoch != atx2.PublishEpoch {
		return nil, fmt.Errorf("ATXs have different publish epoch (%v != %v)", atx1.PublishEpoch, atx2.PublishEpoch)
	}
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}
	if atx1.MarriageATX == nil {
		return nil, errors.New("ATX 1 have no marriage ATX")
	}
	if atx2.MarriageATX == nil {
		return nil, errors.New("ATX 2 have no marriage ATX")
	}
	if *atx1.MarriageATX != *atx2.MarriageATX {
		return nil, errors.New("ATXs have different marriage ATXs")
	}

	proof1, err := newMergeProof(atx1)
	if err != nil {
		return nil, fmt.Errorf("creating proof for atx1: %w", err)
	}

	proof2, err := newMergeProof(atx2)
	if err != nil {
		return nil, fmt.Errorf("creating proof for atx2: %w", err)
	}

	proof := ProofDoubleMerge{
		PublishEpoch: atx1.PublishEpoch,
		Proofs:       [2]MergeProof{*proof1, *proof2},
		MarriageProof: MarriageProof{
			// TODO: implement marriage proof
			ATXID: *atx1.MarriageATX,
		},
	}

	return &proof, nil
}

// Proof that ATX:
// - was signed by specific smesher
// - had specific publish epoch
// - had specific marriage ATX.
type MergeProof struct {
	// ATXID is the ID of the ATX being proven.
	ATXID types.ATXID
	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature

	// Proof for:
	// - publish epoch
	// - marriage ATX
	FieldsProof []types.Hash32 `scale:"max=32"`
}

func (p *MergeProof) valid(verifier *signing.EdVerifier, publish types.EpochID, marriage types.ATXID) (bool, error) {
	if !verifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return false, nil
	}
	proof := make([][]byte, len(p.FieldsProof))
	for i, h := range p.FieldsProof {
		proof[i] = h.Bytes()
	}

	var publishEpochLeaf types.Hash32
	binary.LittleEndian.PutUint32(publishEpochLeaf[:], publish.Uint32())

	return merkle.ValidatePartialTree(
		[]uint64{uint64(PublishEpochIndex), uint64(MarriageATXIndex)},
		[][]byte{publishEpochLeaf.Bytes(), marriage.Bytes()},
		proof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
}

func newMergeProof(atx *ActivationTxV2) (*MergeProof, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{
			uint64(PublishEpochIndex): true,
			uint64(MarriageATXIndex):  true,
		}).
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

	return &MergeProof{
		ATXID:       atx.ID(),
		SmesherID:   atx.SmesherID,
		Signature:   atx.Signature,
		FieldsProof: proofHashes,
	}, nil
}

// Proof that 2 IDs are married.
type MarriageProof struct {
	// ID of marriage ATX
	ATXID types.ATXID

	// MarriageRoot and its proof that it is contained in the ATX.
	MarriageRoot  types.Hash32
	MarriageProof []types.Hash32 `scale:"max=32"`

	// Proof that marriage certificates were included in ATX.
	CertificatesData [2]MarriageCertificateData
	CertificateProof []types.Hash32 `scale:"max=32"`
}

func (p *MarriageProof) valid() (bool, error) {
	// TODO: implement
	return true, nil
}

type MarriageCertificateData struct {
	Reference types.ATXID
	Spouse    types.NodeID
	Signature types.EdSignature
	Index     uint64
}
