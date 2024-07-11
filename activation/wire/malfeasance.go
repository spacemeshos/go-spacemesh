package wire

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

// MerkleTreeIndex is the index of the leaf containing the given field in the merkle tree.
type MerkleTreeIndex uint16

const (
	PublishEpochIndex MerkleTreeIndex = iota
	PositioningATXIndex
	CoinbaseIndex
	InitialPostIndex
	PreviousATXsRootIndex
	NIPostsRootIndex
	VRFNonceIndex
	MarriagesRootIndex
	MarriageATXIndex
)

// ProofType is an identifier for the type of proof that is encoded in the ATXProof.
type ProofType byte

const (
	DoublePublish ProofType = iota + 1
	DoubleMarry
)

type ATXProof struct {
	// LayerID is the layer in which the proof was created. This can be used to implement different versions of the ATX
	// proof in the future.
	Layer types.LayerID
	// ProofType is the type of proof that is being provided.
	ProofType ProofType
	// Certificates is a slice of marriage certificates showing which identities belong to the same marriage set as
	// the one proven to be malfeasant. Up to 1024 can be put into a single proof, since by repeatedly marrying other
	// identities there can be much more than 256 in a malfeasant marriage set. Beyond that a second proof could be
	// provided to show that additional identities are part of the same malfeasant marriage set.
	Certificates []ProofCertificate `scale:"max=1024"`

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

// Proof is an interface for all types of proofs that can be provided in an ATXProof.
// Generally the proof should be able to validate itself.
type Proof interface {
	Valid(edVerifier *signing.EdVerifier) (types.NodeID, error)
}
