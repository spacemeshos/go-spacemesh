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
	PublishEpoch types.EpochID
	Proofs       [2]MergeProof
	// TODO: what if IDs married before checkpoint?
	// We would not be able to construct marriage proof, but everybody
	// on the network must agree that both IDs are married (it's persisted in checkpoint)
	MarriageProof MarriageProof
}

var _ Proof = &ProofDoubleMerge{}

// Valid implements Proof.Valid.
func (p *ProofDoubleMerge) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	// 1. The ATXs have different IDs.
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("ATXs have the same ID")
	}

	// 2. Both ATXs have a valid signature.
	if !edVerifier.Verify(signing.ATX, p.Proofs[0].SmesherID, p.Proofs[0].ATXID.Bytes(), p.Proofs[0].Signature) {
		return types.EmptyNodeID, errors.New("ATX 1 invalid signature")
	}

	if !edVerifier.Verify(signing.ATX, p.Proofs[1].SmesherID, p.Proofs[1].ATXID.Bytes(), p.Proofs[1].Signature) {
		return types.EmptyNodeID, errors.New("ATX 2 invalid signature")
	}

	// 3. and 4. (publish epoch and marriage ATX)
	err := p.Proofs[0].valid(edVerifier, p.PublishEpoch, p.MarriageProof.ATXID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating ATX 1 merge proof: %w", err)
	}

	err = p.Proofs[1].valid(edVerifier, p.PublishEpoch, p.MarriageProof.ATXID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating ATX 2 merge proof: %w", err)
	}

	// 5. signers are married
	err = p.MarriageProof.valid(edVerifier, p.Proofs[0].SmesherID, p.Proofs[1].SmesherID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating marriage proof: %w", err)
	}

	return p.Proofs[0].SmesherID, nil
}

func NewDoubleMergeProof(db sql.Executor, atx1, atx2, marriageATX *ActivationTxV2) (*ProofDoubleMerge, error) {
	if atx1.PublishEpoch != atx2.PublishEpoch {
		return nil, fmt.Errorf("ATXs have different publish epoch (%v != %v)", atx1.PublishEpoch, atx2.PublishEpoch)
	}
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}
	if atx1.SmesherID == atx2.SmesherID {
		return nil, errors.New("ATXs have the same smesher")
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

	marriageProof, err := newMarriageProof(db, marriageATX, atx1.SmesherID, atx2.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("creating marriage proof: %w", err)
	}
	proof := ProofDoubleMerge{
		PublishEpoch:  atx1.PublishEpoch,
		Proofs:        [2]MergeProof{*proof1, *proof2},
		MarriageProof: *marriageProof,
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

func (p *MergeProof) valid(verifier *signing.EdVerifier, publish types.EpochID, marriage types.ATXID) error {
	if !verifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid ATX signature")
	}
	proof := make([][]byte, len(p.FieldsProof))
	for i, h := range p.FieldsProof {
		proof[i] = h.Bytes()
	}

	var publishEpochLeaf types.Hash32
	binary.LittleEndian.PutUint32(publishEpochLeaf[:], publish.Uint32())

	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(PublishEpochIndex), uint64(MarriageATXIndex)},
		[][]byte{publishEpochLeaf.Bytes(), marriage.Bytes()},
		proof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
	if err != nil || !ok {
		return fmt.Errorf("validating merge proof: %w", err)
	}
	return nil
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
	// Marriage ATX ID
	ATXID types.ATXID
	// Smesher who published the marriage ATX
	SmesherID types.NodeID
	// Signature of the marriage ATX
	Signature types.EdSignature

	// MarriageRoot and its proof that it is contained in the ATX.
	MarriageRoot  types.Hash32
	MarriageProof []types.Hash32 `scale:"max=32"`

	// Proof that marriage certificates were included in ATX.
	CertificatesData [2]MarriageCertificateData
	CertificateProof []types.Hash32 `scale:"max=32"`
}

func newMarriageProof(db sql.Executor, marriageATX *ActivationTxV2, id1, id2 types.NodeID) (*MarriageProof, error) {
	marriageProof, err := marriageProof(marriageATX)
	if err != nil {
		return nil, fmt.Errorf("creating marriage certs proof: %w", err)
	}

	marriageIdx1, err := findMarriageIndex(db, marriageATX, id1)
	if err != nil {
		return nil, fmt.Errorf("finding marriage index for: %w", err)
	}

	marriageIdx2, err := findMarriageIndex(db, marriageATX, id2)
	if err != nil {
		return nil, fmt.Errorf("finding marriage index: %w", err)
	}

	certsProof, err := certificateProof(marriageATX.Marriages, uint64(marriageIdx1), uint64(marriageIdx2))
	if err != nil {
		return nil, fmt.Errorf("creating marriage certs proof: %w", err)
	}

	proof := &MarriageProof{
		ATXID:     marriageATX.ID(),
		SmesherID: marriageATX.SmesherID,
		Signature: marriageATX.Signature,

		MarriageRoot:  types.Hash32(marriageATX.Marriages.Root()),
		MarriageProof: marriageProof,

		CertificatesData: [2]MarriageCertificateData{
			{
				Certificate: marriageATX.Marriages[marriageIdx1],
				Index:       uint64(marriageIdx1),
			},
			{
				Certificate: marriageATX.Marriages[marriageIdx2],
				Index:       uint64(marriageIdx2),
			},
		},

		CertificateProof: certsProof,
	}

	return proof, nil
}

func (p *MarriageProof) valid(edVerifier *signing.EdVerifier, id1, id2 types.NodeID) error {
	// 1. Marriage ATX signature
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid ATX signature")
	}

	// 2. ID 1 married marriage ATX smesher
	if !edVerifier.Verify(signing.MARRIAGE, id1, p.SmesherID.Bytes(), p.CertificatesData[0].Certificate.Signature) {
		return errors.New("invalid certificate signature for id1")
	}

	// 3. ID 2 married marriage ATX smesher
	if !edVerifier.Verify(signing.MARRIAGE, id2, p.SmesherID.Bytes(), p.CertificatesData[1].Certificate.Signature) {
		return errors.New("invalid certificate signature for id1")
	}

	// 4. Proof that Marriages with given root were part of `marriageATX`
	proof := make([][]byte, len(p.MarriageProof))
	for i, h := range p.MarriageProof {
		proof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(MarriagesRootIndex)},
		[][]byte{p.MarriageRoot.Bytes()},
		proof,
		p.ATXID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate marriage proof: %w", err)
	}
	if !ok {
		return errors.New("invalid marriage proof")
	}

	// 5. Proof that given marriage certificates were part of Marriages that has p.MarriageRoot
	certProof := make([][]byte, len(p.CertificateProof))
	for i, h := range p.CertificateProof {
		certProof[i] = h.Bytes()
	}

	// indices and respectives leaves must be sorted
	leafIndices := []uint64{p.CertificatesData[0].Index, p.CertificatesData[1].Index}
	leaves := [][]byte{p.CertificatesData[0].Certificate.Root(), p.CertificatesData[1].Certificate.Root()}
	if leafIndices[0] > leafIndices[1] {
		leafIndices[0], leafIndices[1] = leafIndices[1], leafIndices[0]
		leaves[0], leaves[1] = leaves[1], leaves[0]
	}
	ok, err = merkle.ValidatePartialTree(
		leafIndices,
		leaves,
		certProof,
		p.MarriageRoot.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		return fmt.Errorf("validate certificate proof: %w", err)
	}
	if !ok {
		return errors.New("invalid certificate proof")
	}

	return nil
}

type MarriageCertificateData struct {
	Certificate MarriageCertificate
	Index       uint64
}
