package wire

import (
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/merkle-tree"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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
// HINT: this works if the identity that publishes the marriage ATX marries themselves.
type ProofDoubleMarry struct {
	Proofs [2]MarryProof
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

	proof := &ProofDoubleMarry{
		Proofs: [2]MarryProof{proof1, proof2},
	}
	return proof, nil
}

func createMarryProof(db sql.Executor, atx *ActivationTxV2, nodeID types.NodeID) (MarryProof, error) {
	marriageProof, err := marriageProof(atx)
	if err != nil {
		return MarryProof{}, fmt.Errorf("failed to create proof for ATX 1: %w", err)
	}

	marriageIndex := slices.IndexFunc(atx.Marriages, func(cert MarriageCertificate) bool {
		if cert.ReferenceAtx == types.EmptyATXID && atx.SmesherID == nodeID {
			// special case of the self signed certificate of the ATX publisher
			return true
		}
		refATX, err := atxs.Get(db, cert.ReferenceAtx)
		if err != nil {
			return false
		}
		return refATX.SmesherID == nodeID
	})
	if marriageIndex == -1 {
		return MarryProof{}, fmt.Errorf("does not contain a marriage certificate signed by %s", nodeID.ShortString())
	}
	certProof, err := certificateProof(atx.Marriages, uint64(marriageIndex))
	if err != nil {
		return MarryProof{}, fmt.Errorf("failed to create certificate proof for ATX 1: %w", err)
	}

	proof := MarryProof{
		ATXID:  atx.ID(),
		NodeID: nodeID,

		MarriageRoot:  types.Hash32(atx.Marriages.Root()),
		MarriageProof: marriageProof,

		CertificateReference: atx.Marriages[marriageIndex].ReferenceAtx,
		CertificateSignature: atx.Marriages[marriageIndex].Signature,
		CertificateIndex:     uint64(marriageIndex),
		CertificateProof:     certProof,

		SmesherID: atx.SmesherID,
		Signature: atx.Signature,
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

func (p ProofDoubleMarry) Valid(edVerifier *signing.EdVerifier) (types.NodeID, error) {
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("proofs have the same ATX ID")
	}
	if p.Proofs[0].NodeID != p.Proofs[1].NodeID {
		return types.EmptyNodeID, errors.New("proofs have different node IDs")
	}

	if err := p.Proofs[0].Valid(edVerifier); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proofs[1].Valid(edVerifier); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return p.Proofs[0].NodeID, nil
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
	CertificateReference types.ATXID
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

	if !edVerifier.Verify(signing.MARRIAGE, p.NodeID, p.SmesherID.Bytes(), p.CertificateSignature) {
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
		ReferenceAtx: p.CertificateReference,
		Signature:    p.CertificateSignature,
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
