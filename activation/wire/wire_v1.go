package wire

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

//go:generate scalegen

type ActivationTxV1 struct {
	InnerActivationTxV1

	SmesherID types.NodeID
	Signature types.EdSignature

	id types.ATXID
}

// InnerActivationTxV1 is a set of all of an ATX's fields, except the signature. To generate the ATX signature, this
// structure is serialized and signed.
type InnerActivationTxV1 struct {
	NIPostChallengeV1

	Coinbase types.Address
	NumUnits uint32

	NIPost   *NIPostV1
	NodeID   *types.NodeID // only present in initial ATX to make hash of the first InnerActivationTxV2 unique
	VRFNonce *uint64       // only present when the nonce changed (or initial ATX)
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

func (atx *ActivationTxV1) ID() types.ATXID {
	if atx.id == types.EmptyATXID {
		atx.id = types.ATXID(atx.HashInnerBytes())
	}
	return atx.id
}

// TODO(mafa): this can be inlined.
func (atx *ActivationTxV1) Smesher() types.NodeID {
	return atx.SmesherID
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

func postToWireV1(p *types.Post) *PostV1 {
	if p == nil {
		return nil
	}
	return &PostV1{
		Nonce:   p.Nonce,
		Indices: p.Indices,
		Pow:     p.Pow,
	}
}

func niPostToWireV1(n *types.NIPost) *NIPostV1 {
	if n == nil {
		return nil
	}

	return &NIPostV1{
		Membership: MerkleProofV1{
			Nodes:     n.Membership.Nodes,
			LeafIndex: n.Membership.LeafIndex,
		},
		Post: postToWireV1(n.Post),
		PostMetadata: &PostMetadataV1{
			Challenge:     n.PostMetadata.Challenge,
			LabelsPerUnit: n.PostMetadata.LabelsPerUnit,
		},
	}
}

func NIPostChallengeToWireV1(c *types.NIPostChallenge) *NIPostChallengeV1 {
	return &NIPostChallengeV1{
		Publish:        c.PublishEpoch,
		Sequence:       c.Sequence,
		PrevATXID:      c.PrevATXID,
		PositioningATX: c.PositioningATX,
		CommitmentATX:  c.CommitmentATX,
		InitialPost:    postToWireV1(c.InitialPost),
	}
}

func ActivationTxToWireV1(a *types.ActivationTx) *ActivationTxV1 {
	return &ActivationTxV1{
		InnerActivationTxV1: InnerActivationTxV1{
			NIPostChallengeV1: *NIPostChallengeToWireV1(&a.NIPostChallenge),
			Coinbase:          a.Coinbase,
			NumUnits:          a.NumUnits,
			NIPost:            niPostToWireV1(a.NIPost),
			NodeID:            a.NodeID,
			VRFNonce:          (*uint64)(a.VRFNonce),
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
				InitialPost:    postFromWireV1(atx.InitialPost),
			},
			Coinbase: atx.Coinbase,
			NumUnits: atx.NumUnits,
			NIPost:   niPostFromWireV1(atx.NIPost),
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
		InitialPost:    postFromWireV1(ch.InitialPost),
	}
}

func niPostFromWireV1(nipost *NIPostV1) *types.NIPost {
	if nipost == nil {
		return nil
	}

	return &types.NIPost{
		Membership: types.MerkleProof{
			LeafIndex: nipost.Membership.LeafIndex,
			Nodes:     nipost.Membership.Nodes,
		},
		Post: postFromWireV1(nipost.Post),
		PostMetadata: &types.PostMetadata{
			Challenge:     nipost.PostMetadata.Challenge,
			LabelsPerUnit: nipost.PostMetadata.LabelsPerUnit,
		},
	}
}

func postFromWireV1(post *PostV1) *types.Post {
	if post == nil {
		return nil
	}
	return &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}
}
