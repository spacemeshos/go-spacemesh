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

// MarryProof is a proof that a NodeID is married to another NodeID.
type MarryProof struct {
	// MarriageCertificatesRoot and its proof that it is contained in the ATX.
	MarriageCertificatesRoot  MarriageCertificatesRoot
	MarriageCertificatesProof MarriageCertificatesRootProof `scale:"max=32"`

	// The signature of the certificate and the proof that the certificate is contained in the MarriageRoot at
	// the given index.
	Certificate      MarriageCertificate
	CertificateProof MarriageCertificateProof `scale:"max=32"`
	CertificateIndex uint32
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
		MarriageCertificatesRoot:  atx.Marriages.Root(),
		MarriageCertificatesProof: atx.MarriagesRootProof(),

		Certificate:      atx.Marriages[marriageIndex],
		CertificateProof: atx.Marriages.Proof(marriageIndex),
		CertificateIndex: uint32(marriageIndex),
	}
	return proof, nil
}

// Valid returns an error if the proof is invalid. It checks that `nodeID` signed a certificate to marry `smesherID`
// and it was included in the ATX with the given `atxID`.
func (p MarryProof) Valid(
	malValidator MalfeasanceValidator,
	atxID types.ATXID,
	smesherID types.NodeID,
	nodeID types.NodeID,
) error {
	if !malValidator.Signature(signing.MARRIAGE, nodeID, smesherID.Bytes(), p.Certificate.Signature) {
		return errors.New("invalid certificate signature")
	}
	if !p.MarriageCertificatesProof.Valid(atxID, p.MarriageCertificatesRoot) {
		return errors.New("invalid marriage proof")
	}
	if !p.CertificateProof.Valid(p.MarriageCertificatesRoot, int(p.CertificateIndex), p.Certificate) {
		return errors.New("invalid certificate proof")
	}
	return nil
}

// MarriageProof is a proof for two identities to be married via a marriage ATX.
type MarriageProof struct {
	// MarriageATX and its proof that it is contained in the ATX.
	MarriageATX      types.ATXID
	MarriageATXProof MarriageATXProof `scale:"max=32"`
	// MarriageATXSmesherID is the ID of the smesher that published the marriage ATX.
	MarriageATXSmesherID types.NodeID

	// NodeIDMarryProof is the proof that NodeID married in MarriageATX.
	NodeIDMarryProof MarryProof
	// SmesherIDMarryProof is the proof that SmesherID married in MarriageATX.
	SmesherIDMarryProof MarryProof
}

func createMarriageProof(db sql.Executor, atx *ActivationTxV2, nodeID types.NodeID) (MarriageProof, error) {
	if nodeID == atx.SmesherID {
		// we don't need a marriage proof if the node ID is the same as the smesher ID
		return MarriageProof{}, errors.New("node ID is the same as smesher ID")
	}

	var blob sql.Blob
	v, err := atxs.LoadBlob(context.Background(), db, atx.MarriageATX.Bytes(), &blob)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("get marriage ATX: %w", err)
	}
	if v != types.AtxV2 {
		return MarriageProof{}, errors.New("invalid ATX version for marriage ATX")
	}
	marriageATX, err := DecodeAtxV2(blob.Bytes)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("decode marriage ATX: %w", err)
	}

	nodeIDmarriageProof, err := createMarryProof(db, marriageATX, nodeID)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("NodeID marriage proof: %w", err)
	}
	smesherIDmarriageProof, err := createMarryProof(db, marriageATX, atx.SmesherID)
	if err != nil {
		return MarriageProof{}, fmt.Errorf("SmesherID marriage proof: %w", err)
	}

	proof := MarriageProof{
		MarriageATX:      marriageATX.ID(),
		MarriageATXProof: atx.MarriageATXProof(),

		MarriageATXSmesherID: marriageATX.SmesherID,

		NodeIDMarryProof:    nodeIDmarriageProof,
		SmesherIDMarryProof: smesherIDmarriageProof,
	}
	return proof, nil
}

func (p MarriageProof) Valid(
	malValidator MalfeasanceValidator,
	atxID types.ATXID,
	nodeID, smesherID types.NodeID,
) error {
	if !p.MarriageATXProof.Valid(atxID, p.MarriageATX) {
		return errors.New("invalid marriage ATX proof")
	}
	if err := p.NodeIDMarryProof.Valid(malValidator, p.MarriageATX, p.MarriageATXSmesherID, nodeID); err != nil {
		return fmt.Errorf("invalid marriage proof for NodeID: %w", err)
	}
	if err := p.SmesherIDMarryProof.Valid(malValidator, p.MarriageATX, p.MarriageATXSmesherID, smesherID); err != nil {
		return fmt.Errorf("invalid marriage proof for SmesherID: %w", err)
	}
	return nil
}
