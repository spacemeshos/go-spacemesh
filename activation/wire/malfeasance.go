package wire

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
)

// MerkleTreeIndex is the index of the leaf containing the given field in the merkle tree.
type MerkleTreeIndex uint64

const (
	PublishEpochIndex MerkleTreeIndex = iota
	PositioningATXIndex
	CoinbaseIndex
	InitialPostRootIndex
	PreviousATXsRootIndex
	NIPostsRootIndex
	VRFNonceIndex
	MarriagesRootIndex
	MarriageATXIndex
)

type InitialPostTreeIndex uint64

const (
	CommitmentATXIndex InitialPostTreeIndex = iota
	InitialPostIndex
)

type NIPostTreeIndex uint64

const (
	MembershipIndex NIPostTreeIndex = iota
	ChallengeIndex
	PostsRootIndex
)

type MarriageCertificateIndex uint64

const (
	ReferenceATXIndex MarriageCertificateIndex = iota
	SignatureIndex
)

type SubPostTreeIndex uint64

const (
	MarriageIndex SubPostTreeIndex = iota
	PrevATXIndex
	MembershipLeafIndex
	PostIndex
	NumUnitsIndex
)

// ProofType is an identifier for the type of proof that is encoded in the ATXProof.
type ProofType byte

const (
	// TODO(mafa): legacy types for future migration to new malfeasance proofs.
	LegacyDoublePublish  ProofType = 0x01
	LegacyInvalidPost    ProofType = 0x02
	LegacyInvalidPrevATX ProofType = 0x03

	DoubleMarry       ProofType = 0x11
	DoubleMerge       ProofType = 0x12
	InvalidPost       ProofType = 0x13
	InvalidPreviousV1 ProofType = 0x14
	InvalidPreviousV2 ProofType = 0x15
)

var proofTypes = map[ProofType]Proof{
	// TODO(mafa): legacy proofs

	DoubleMarry:       &ProofDoubleMarry{},
	DoubleMerge:       &ProofDoubleMerge{},
	InvalidPost:       &ProofInvalidPost{},
	InvalidPreviousV1: &ProofInvalidPrevAtxV1{},
	InvalidPreviousV2: &ProofInvalidPrevAtxV2{},
}

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

func (p *ATXProof) Decode() (Proof, error) {
	rst, ok := proofTypes[p.ProofType]
	if !ok {
		return nil, fmt.Errorf("unknown ATX malfeasance proof type: 0x%x", p.ProofType)
	}
	if err := codec.Decode(p.Proof, rst); err != nil {
		return nil, fmt.Errorf("decoding ATX malfeasance proof of type 0x%x: %w", p.ProofType, err)
	}
	return rst, nil
}
