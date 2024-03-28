package wire

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types/primitive"
	"github.com/spacemeshos/go-spacemesh/hash"
)

//go:generate scalegen

type VRFPostIndex uint64

func (v *VRFPostIndex) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact64(enc, uint64(*v))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (v *VRFPostIndex) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		value, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		*v = VRFPostIndex(value)
	}
	return total, nil
}

type ActivationTxV1 struct {
	InnerActivationTxV1

	SmesherID primitive.Hash32
	Signature [64]byte
}

type InnerActivationTxV1 struct {
	NIPostChallengeV1

	Coinbase Address
	NumUnits uint32

	NIPost   *NIPostV1
	NodeID   *primitive.Hash32 // only present in initial ATX to make hash of the first InnerActivationTxV2 unique
	VRFNonce *VRFPostIndex     // only present when the nonce changed (or initial ATX)
}

type NIPostChallengeV1 struct {
	PublishEpoch uint32
	// Sequence number counts the number of ancestors of the ATX. It sequentially increases for each ATX in the chain.
	// Two ATXs with the same sequence number from the same miner can be used as the proof of malfeasance against
	// that miner.
	Sequence uint64
	// the previous ATX's ID (for all but the first in the sequence)
	PrevATXID      primitive.Hash32
	PositioningATX primitive.Hash32

	// CommitmentATX is the ATX used in the commitment for initializing the PoST of the node.
	CommitmentATX *primitive.Hash32
	InitialPost   *PostV1
}

// Hash serializes the NIPostChallenge and returns its hash.
// The serialized challenge is first prepended with a byte 0x00, and then hashed
// for second preimage resistance of poet membership merkle tree.
func (challenge *NIPostChallengeV1) Hash() primitive.Hash32 {
	ncBytes := codec.MustEncode(challenge)
	return hash.Sum([]byte{0x00}, ncBytes)
}

type PostV1 struct {
	Nonce   uint32
	Indices []byte `scale:"max=800"` // up to K2=100
	Pow     uint64
}

type MerkleProofV1 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes     []primitive.Hash32 `scale:"max=32"`
	LeafIndex uint64
}

type NIPostV1 struct {
	// Membership proves that the challenge for the PoET, which is
	// constructed from fields in the activation transaction,
	// is a member of the poet's proof.
	// Proof.Root must match the Poet's POSW statement.
	Membership MerkleProofV1

	// Post is the proof that the prover data is still stored (or was recomputed) at
	// the time he learned the challenge constructed from the PoET.
	Post *PostV1

	// PostMetadata is the Post metadata, associated with the proof.
	// The proof should be verified upon the metadata during the syntactic validation,
	// while the metadata should be verified during the contextual validation.
	PostMetadata *PostMetadataV1
}

type PostMetadataV1 struct {
	Challenge     []byte `scale:"max=32"`
	LabelsPerUnit uint64
}

type ATXMetadataV1 struct {
	PublishEpoch uint32
	MsgHash      primitive.Hash32
}

func (atx *ActivationTxV1) SignedBytes() []byte {
	data := codec.MustEncode(&ATXMetadataV1{
		PublishEpoch: atx.PublishEpoch,
		MsgHash:      atx.HashInnerBytes(),
	})
	return data
}

func (atx *ActivationTxV1) HashInnerBytes() (result primitive.Hash32) {
	h := hash.New()
	codec.MustEncodeTo(h, &atx.InnerActivationTxV1)
	h.Sum(result[:0])
	return result
}
