package wire

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

// ProofType is an identifier for the type of proof that is encoded in the ATXProof.
type ProofType byte

const (
	DoublePublish ProofType = iota + 1
	DoubleMarry
	InvalidPost
)

type ATXProof struct {
	// LayerID is the layer in which the proof was created. This can be used to implement different versions of the ATX
	// proof in the future.
	Layer types.LayerID
	// ProofType is the type of proof that is being provided.
	ProofType ProofType
	// Proof is the actual proof. Its type depends on the ProofType.
	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}

// ProofCertificate proofs that two identities are part of the same marriage set.
type ProofCertificate struct {
	// Target of the certificate, i.e. the identity that signed the ATX containing the original certificate.
	Target types.NodeID
	// ID is the identity that signed the certificate.
	ID types.NodeID
	// Signature is the signature of the certificate, i.e. ID signed with the key of SmesherID.
	Signature types.EdSignature
}
