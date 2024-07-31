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

type InitialPostTreeIndex uint64

const (
	CommitmentATXIndex InitialPostTreeIndex = iota
	InitialPostRootIndex
)

type NiPostTreeIndex uint64

const (
	MembershipIndex NiPostTreeIndex = iota
	ChallengeIndex
	PostsRootIndex
)

// ProofType is an identifier for the type of proof that is encoded in the ATXProof.
type ProofType byte

const (
	DoublePublish ProofType = iota + 1
	DoubleMarry
	InvalidPost
)

// ProofVersion is an identifier for the version of the proof that is encoded in the ATXProof.
type ProofVersion byte

type ATXProof struct {
	// Version is the version identifier of the proof. This can be used to extend the ATX proof in the future.
	Version ProofVersion
	// ProofType is the type of proof that is being provided.
	ProofType ProofType
	// Proof is the actual proof. Its type depends on the ProofType.
	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}

// Proof is an interface for all types of proofs that can be provided in an ATXProof.
// Generally the proof should be able to validate itself.
type Proof interface {
	Valid(edVerifier *signing.EdVerifier) (types.NodeID, error)
}
