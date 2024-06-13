package wire

import (
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

// ProofDoubleMarry is a proof that two distinct ATXs contain a marriage certificate signed by the same identity.
//
// We are proofing the following:
// 1. The ATXs have different IDs.
// 2. Both ATXs have a valid signature.
// 3. Both ATXs contain a marriage certificate created by the same identity.
// 4. Both marriage certificates have valid signatures.
//
// HINT: this only works if the identity that publishes the ATX with the certificates marries itself.
type ProofDoubleMarry struct {
	Proofs [2]MarryProof
}

func NewDoubleMarryProof(atx1, atx2 *ActivationTxV2, nodeID types.NodeID) (*ProofDoubleMarry, error) {
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}

	atx1Index := slices.IndexFunc(atx1.Marriages, func(cert MarriageCertificate) bool {
		return cert.ID == nodeID
	})
	if atx1Index == -1 {
		return nil, errors.New("ATX 1 does not contain a marriage certificate signed by the given node ID")
	}
	atx2Index := slices.IndexFunc(atx2.Marriages, func(cert MarriageCertificate) bool {
		return cert.ID == nodeID
	})
	if atx2Index == -1 {
		return nil, errors.New("ATX 2 does not contain a marriage certificate signed by the given node ID")
	}

	proof1, err := marriageProof(atx1)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof for ATX 1: %w", err)
	}

	proof2, err := marriageProof(atx2)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof for ATX 2: %w", err)
	}

	certProof1, err := certificateProof(atx1.Marriages, uint64(atx1Index))
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate proof for ATX 1: %w", err)
	}

	certProof2, err := certificateProof(atx2.Marriages, uint64(atx2Index))
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate proof for ATX 2: %w", err)
	}

	proof := &ProofDoubleMarry{
		Proofs: [2]MarryProof{
			{
				ATXID:  atx1.ID(),
				NodeID: nodeID,

				MarriageRoot:  types.Hash32(atx1.Marriages.Root()),
				MarriageProof: proof1,

				CertificateSignature: atx1.Marriages[atx1Index].Signature,
				CertificateIndex:     uint64(atx1Index),
				CertificateProof:     certProof1,

				SmesherID: atx1.SmesherID,
				Signature: atx1.Signature,
			},
			{
				ATXID:  atx2.ID(),
				NodeID: nodeID,

				MarriageRoot:  types.Hash32(atx2.Marriages.Root()),
				MarriageProof: proof2,

				CertificateSignature: atx2.Marriages[atx2Index].Signature,
				CertificateIndex:     uint64(atx2Index),
				CertificateProof:     certProof2,

				SmesherID: atx2.SmesherID,
				Signature: atx2.Signature,
			},
		},
	}
	return proof, nil
}

func marriageProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(MarriagesRootIndex): true}).
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

func certificateProof(certs MarriageCertificates, index uint64) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{index: true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	certs.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes, nil
}

func (p ProofDoubleMarry) Valid(edVerifier *signing.EdVerifier) error {
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return errors.New("proofs have the same ATX ID")
	}
	if p.Proofs[0].NodeID != p.Proofs[1].NodeID {
		return errors.New("proofs have different node IDs")
	}

	if err := p.Proofs[0].Valid(edVerifier); err != nil {
		return fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proofs[1].Valid(edVerifier); err != nil {
		return fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return nil
}

type MarryProof struct {
	// ATXID is the ID of the ATX being proven.
	ATXID types.ATXID
	// NodeID is the node ID that married twice.
	NodeID types.NodeID

	// MarriageRoot and its proof that it is contained in the ATX.
	MarriageRoot  types.Hash32
	MarriageProof []types.Hash32 `scale:"max=32"`

	// The signature of the certificate and the proof that the certificate is contained in the MarriageRoot at
	// the given index.
	CertificateSignature types.EdSignature
	CertificateIndex     uint64
	CertificateProof     []types.Hash32 `scale:"max=32"`

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

func (p MarryProof) Valid(edVerifier *signing.EdVerifier) error {
	if !edVerifier.Verify(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid ATX signature")
	}

	// TODO(mafa): check domain
	if !edVerifier.Verify(signing.ATX, p.NodeID, p.SmesherID.Bytes(), p.CertificateSignature) {
		return errors.New("invalid certificate signature")
	}

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

	mc := MarriageCertificate{
		ID:        p.NodeID,
		Signature: p.CertificateSignature,
	}

	certProof := make([][]byte, len(p.CertificateProof))
	for i, h := range p.CertificateProof {
		certProof[i] = h.Bytes()
	}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{p.CertificateIndex},
		[][]byte{mc.Root()},
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
