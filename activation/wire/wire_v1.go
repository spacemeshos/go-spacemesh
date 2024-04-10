package wire

import (
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

	SmesherID types.NodeID
	Signature [64]byte
}

// InnerActivationTxV1 is a set of all of an ATX's fields, except the signature. To generate the ATX signature, this
// structure is serialized and signed.
type InnerActivationTxV1 struct {
	NIPostChallengeV1

	Coinbase types.Address
	NumUnits uint32

	NIPost   *NIPostV1
	NodeID   *types.NodeID // only present in initial ATX to make hash of the first InnerActivationTxV2 unique
	VRFNonce *VRFPostIndex // only present when the nonce changed (or initial ATX)
}

type NIPostChallengeV1 struct {
	Publish types.EpochID
	// Sequence number counts the number of ancestors of the ATX. It sequentially increases for each ATX in the chain.
	// Two ATXs with the same sequence number from the same miner can be used as the proof of malfeasance against
	// that miner.
	Sequence uint64
	// the previous ATX's ID (for all but the first in the sequence)
	PrevATXID      types.ATXID
	PositioningATX types.ATXID

	// CommitmentATX is the ATX used in the commitment for initializing the PoST of the node.
	CommitmentATX *types.ATXID
	InitialPost   *PostV1
}

// Hash serializes the NIPostChallenge and returns its hash.
// The serialized challenge is first prepended with a byte 0x00, and then hashed
// for second preimage resistance of poet membership merkle tree.
func (challenge *NIPostChallengeV1) Hash() types.Hash32 {
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
	Nodes     []types.Hash32 `scale:"max=32"`
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
	Publish types.EpochID
	MsgHash types.Hash32
}

func (atx *ActivationTxV1) SignedBytes() []byte {
	data := codec.MustEncode(&ATXMetadataV1{
		Publish: atx.Publish,
		MsgHash: atx.HashInnerBytes(),
	})
	return data
}

func (atx *ActivationTxV1) HashInnerBytes() (result types.Hash32) {
	h := hash.New()
	codec.MustEncodeTo(h, &atx.InnerActivationTxV1)
	h.Sum(result[:0])
	return result
}

func PostToWireV1(p *types.Post) *PostV1 {
	if p == nil {
		return nil
	}
	return &PostV1{
		Nonce:   p.Nonce,
		Indices: p.Indices,
		Pow:     p.Pow,
	}
}

func NIPostToWireV1(n *types.NIPost) *NIPostV1 {
	if n == nil {
		return nil
	}

	return &NIPostV1{
		Membership: *MerkleProofToWireV1(n.Membership),
		Post:       PostToWireV1(n.Post),
		PostMetadata: &PostMetadataV1{
			Challenge:     n.PostMetadata.Challenge,
			LabelsPerUnit: n.PostMetadata.LabelsPerUnit,
		},
	}
}

func MerkleProofToWireV1(p types.MerkleProof) *MerkleProofV1 {
	return &MerkleProofV1{
		Nodes:     p.Nodes,
		LeafIndex: p.LeafIndex,
	}
}

func NIPostChallengeToWireV1(c *types.NIPostChallenge) *NIPostChallengeV1 {
	return &NIPostChallengeV1{
		Publish:        c.PublishEpoch,
		Sequence:       c.Sequence,
		PrevATXID:      c.PrevATXID,
		PositioningATX: c.PositioningATX,
		CommitmentATX:  c.CommitmentATX,
		InitialPost:    PostToWireV1(c.InitialPost),
	}
}

func ActivationTxToWireV1(a *types.ActivationTx) *ActivationTxV1 {
	return &ActivationTxV1{
		InnerActivationTxV1: InnerActivationTxV1{
			NIPostChallengeV1: *NIPostChallengeToWireV1(&a.NIPostChallenge),
			Coinbase:          a.Coinbase,
			NumUnits:          a.NumUnits,
			NIPost:            NIPostToWireV1(a.NIPost),
			NodeID:            a.NodeID,
			VRFNonce:          (*VRFPostIndex)(a.VRFNonce),
		},
		SmesherID: a.SmesherID,
		Signature: a.Signature,
	}
}

// Decode ActivationTx from bytes.
// In future it should decide which version of ActivationTx to decode based on the publish epoch.
func ActivationTxFromBytes(data []byte) (*types.ActivationTx, error) {
	var wireAtx ActivationTxV1
	err := codec.Decode(data, &wireAtx)
	if err != nil {
		return nil, fmt.Errorf("decoding ATX: %w", err)
	}

	return ActivationTxFromWireV1(&wireAtx), nil
}

func ActivationTxFromWireV1(atx *ActivationTxV1) *types.ActivationTx {
	result := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   atx.Publish,
				Sequence:       atx.Sequence,
				PrevATXID:      atx.PrevATXID,
				PositioningATX: atx.PositioningATX,
				CommitmentATX:  atx.CommitmentATX,
				InitialPost:    PostFromWireV1(atx.InitialPost),
			},
			Coinbase: atx.Coinbase,
			NumUnits: atx.NumUnits,
			NIPost:   NIPostFromWireV1(atx.NIPost),
			NodeID:   atx.NodeID,
			VRFNonce: (*types.VRFPostIndex)(atx.VRFNonce),
		},
		SmesherID: atx.SmesherID,
		Signature: atx.Signature,
	}

	result.SetID(types.ATXID(atx.HashInnerBytes()))
	return result
}

func NIPostChallengeFromWireV1(ch NIPostChallengeV1) *types.NIPostChallenge {
	return &types.NIPostChallenge{
		PublishEpoch:   ch.Publish,
		Sequence:       ch.Sequence,
		PrevATXID:      ch.PrevATXID,
		PositioningATX: ch.PositioningATX,
		CommitmentATX:  ch.CommitmentATX,
		InitialPost:    PostFromWireV1(ch.InitialPost),
	}
}

func NIPostFromWireV1(nipost *NIPostV1) *types.NIPost {
	if nipost == nil {
		return nil
	}

	return &types.NIPost{
		Membership: *MerkleProofFromWireV1(nipost.Membership),
		Post:       PostFromWireV1(nipost.Post),
		PostMetadata: &types.PostMetadata{
			Challenge:     nipost.PostMetadata.Challenge,
			LabelsPerUnit: nipost.PostMetadata.LabelsPerUnit,
		},
	}
}

func PostFromWireV1(post *PostV1) *types.Post {
	if post == nil {
		return nil
	}
	return &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}
}

func MerkleProofFromWireV1(proofV1 MerkleProofV1) *types.MerkleProof {
	return &types.MerkleProof{
		LeafIndex: proofV1.LeafIndex,
		Nodes:     proofV1.Nodes,
	}
}
