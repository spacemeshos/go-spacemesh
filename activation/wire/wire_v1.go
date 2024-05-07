package wire

import (
	"encoding/hex"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
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

func (atx *ActivationTxV1) Smesher() types.NodeID {
	return atx.SmesherID
}

func (atx *ActivationTxV1) Sign(signer *signing.EdSigner) {
	if atx.PrevATXID == types.EmptyATXID {
		nodeID := signer.NodeID()
		atx.NodeID = &nodeID
	}
	atx.Signature = signer.Sign(signing.ATX, atx.SignedBytes())
	atx.SmesherID = signer.NodeID()
}

func (atx *ActivationTxV1) SignedBytes() []byte {
	data := codec.MustEncode(&ATXMetadataV1{
		Publish: atx.PublishEpoch,
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

func NiPostToWireV1(n *types.NIPost) *NIPostV1 {
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
		PublishEpoch:     c.PublishEpoch,
		Sequence:         c.Sequence,
		PrevATXID:        c.PrevATXID,
		PositioningATXID: c.PositioningATX,
		CommitmentATXID:  c.CommitmentATX,
		InitialPost:      postToWireV1(c.InitialPost),
	}
}

func ActivationTxToWireV1(a *types.ActivationTx) *ActivationTxV1 {
	return &ActivationTxV1{
		InnerActivationTxV1: InnerActivationTxV1{
			NIPostChallengeV1: NIPostChallengeV1{
				PublishEpoch:    a.PublishEpoch,
				Sequence:        a.Sequence,
				PrevATXID:       a.PrevATXID,
				CommitmentATXID: a.CommitmentATX,
			},
			Coinbase: a.Coinbase,
			NumUnits: a.NumUnits,
			VRFNonce: (*uint64)(a.VRFNonce),
		},
		SmesherID: a.SmesherID,
	}
}

func ActivationTxFromWireV1(atx *ActivationTxV1, blob ...byte) *types.ActivationTx {
	result := &types.ActivationTx{
		PublishEpoch:  atx.PublishEpoch,
		Sequence:      atx.Sequence,
		PrevATXID:     atx.PrevATXID,
		CommitmentATX: atx.CommitmentATXID,
		Coinbase:      atx.Coinbase,
		NumUnits:      atx.NumUnits,
		VRFNonce:      (*types.VRFPostIndex)(atx.VRFNonce),
		SmesherID:     atx.SmesherID,
		AtxBlob: types.AtxBlob{
			Version: types.AtxV1,
			Blob:    blob,
		},
	}
	if len(blob) == 0 {
		result.AtxBlob.Blob = codec.MustEncode(atx)
	}

	result.SetID(types.ATXID(atx.HashInnerBytes()))
	return result
}

func NiPostFromWireV1(nipost *NIPostV1) *types.NIPost {
	if nipost == nil {
		return nil
	}

	return &types.NIPost{
		Membership: types.MerkleProof{
			LeafIndex: nipost.Membership.LeafIndex,
			Nodes:     nipost.Membership.Nodes,
		},
		Post: PostFromWireV1(nipost.Post),
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

func (p *PostV1) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddUint32("nonce", p.Nonce)
	encoder.AddUint64("k2pow", p.Pow)
	encoder.AddString("indices", hex.EncodeToString(p.Indices))
	return nil
}

func (nipost *NIPostV1) MarshalLogObject(encoder log.ObjectEncoder) error {
	if nipost == nil {
		return nil
	}
	encoder.AddObject("post", nipost.Post)
	encoder.AddBinary("challenge", nipost.PostMetadata.Challenge)
	return nil
}

func (atx *ActivationTxV1) MarshalLogObject(encoder log.ObjectEncoder) error {
	if atx == nil {
		return nil
	}
	encoder.AddString("atx_id", atx.ID().String())
	encoder.AddString("smesher", atx.SmesherID.String())
	encoder.AddString("coinbase", atx.Coinbase.String())
	encoder.AddUint64("num_units", uint64(atx.NumUnits))
	if atx.VRFNonce != nil {
		encoder.AddUint64("vrf_nonce", uint64(*atx.VRFNonce))
	}
	encoder.AddObject("challenge", &atx.NIPostChallengeV1)
	encoder.AddObject("nipost", atx.NIPost)
	return nil
}
