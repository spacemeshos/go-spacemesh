package wire

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

//go:generate scalegen

// ProofDoubleMarry is a proof that two distinct ATXs contain a marriage certificate signed by the same identity.
//
// We are proving the following:
// 1. The ATXs have different IDs.
// 2. Both ATXs have a valid signature.
// 3. Both ATXs contain a marriage certificate created by the same identity.
// 4. Both marriage certificates have valid signatures.
//
// HINT: this works if the identity that publishes the marriage ATX marries themselves.
type ProofDoubleMarry struct {
	// NodeID is the node ID that married twice.
	NodeID types.NodeID

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
		NodeID: nodeID,
		Proofs: [2]MarryProof{proof1, proof2},
	}
	return proof, nil
}

func (p ProofDoubleMarry) Valid(_ context.Context, malValidator MalfeasanceValidator) (types.NodeID, error) {
	if p.Proofs[0].ATXID == p.Proofs[1].ATXID {
		return types.EmptyNodeID, errors.New("proofs have the same ATX ID")
	}

	if err := p.Proofs[0].Valid(malValidator, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 1 is invalid: %w", err)
	}
	if err := p.Proofs[1].Valid(malValidator, p.NodeID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("proof 2 is invalid: %w", err)
	}
	return p.NodeID, nil
}

type MarryProof struct {
	// ATXID is the ID of the ATX being proven to have the marriage certificate of interest.
	ATXID types.ATXID

	// MarriageCertificatesRoot and its proof that it is contained in the ATX.
	MarriageCertificatesRoot  MarriageCertificatesRoot
	MarriageCertificatesProof MarriageCertificatesRootProof `scale:"max=32"`

	// The signature of the certificate and the proof that the certificate is contained in the MarriageRoot at
	// the given index.
	Certificate      MarriageCertificate
	CertificateProof MarriageCertificateProof `scale:"max=32"`
	CertificateIndex uint32

	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID types.NodeID
	// Signature is the signature of the ATXID by the smesher.
	Signature types.EdSignature
}

func createMarryProof(db sql.Executor, atx *ActivationTxV2, nodeID types.NodeID) (MarryProof, error) {
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

	proof := MarryProof{
		ATXID: atx.ID(),

		MarriageCertificatesRoot:  atx.Marriages.Root(),
		MarriageCertificatesProof: atx.MarriagesRootProof(),

		Certificate:      atx.Marriages[marriageIndex],
		CertificateProof: atx.Marriages.Proof(marriageIndex),
		CertificateIndex: uint32(marriageIndex),

		SmesherID: atx.SmesherID,
		Signature: atx.Signature,
	}
	return proof, nil
}

func (p MarryProof) Valid(malValidator MalfeasanceValidator, nodeID types.NodeID) error {
	if !malValidator.Signature(signing.ATX, p.SmesherID, p.ATXID.Bytes(), p.Signature) {
		return errors.New("invalid ATX signature")
	}

	if !malValidator.Signature(signing.MARRIAGE, nodeID, p.SmesherID.Bytes(), p.Certificate.Signature) {
		return errors.New("invalid certificate signature")
	}

	if !p.MarriageCertificatesProof.Valid(p.ATXID, p.MarriageCertificatesRoot) {
		return errors.New("invalid marriage proof")
	}

	if !p.CertificateProof.Valid(p.MarriageCertificatesRoot, int(p.CertificateIndex), p.Certificate) {
		return errors.New("invalid certificate proof")
	}
	return nil
}
