package wire

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

type ActivationTxV2 struct {
	PublishEpoch   types.EpochID
	PositioningATX types.ATXID

	// Must be present in the initial ATX.
	// Nil in subsequent ATXs unless smesher wants to change it.
	// If Nil, the value is inferred from the previous ATX of this smesher.
	// It's not allowed to pass the same coinbase as already used by the previous ATX
	// to avoid spamming the network with redundant information.
	Coinbase *types.Address

	// only present in initial ATX
	Initial      *InitialAtxPartsV2
	PreviousATXs []types.ATXID `scale:"max=256"`
	NiPosts      []NiPostsV2   `scale:"max=2"`

	// The VRF nonce must be valid for the collected space of all included IDs.
	// only present when:
	// - the nonce changed (included more/heavier IDs)
	// - it's an initial ATX
	VRFNonce *uint64

	// The list of marriages with other IDs.
	// A marriage is permanent and cannot be revoked or repeated.
	// All new IDs that are married to this ID are added to the equivocation set
	// that this ID belongs to.
	Marriages []MarriageCertificate `scale:"max=256"`

	// The ID of the ATX containing marriage for the included IDs.
	// Only required when the ATX includes married IDs.
	MarriageATX *types.ATXID

	Signature types.EdSignature
}

// The first PoST is always for the ATX owner.
func (atx *ActivationTxV2) SmesherID() types.NodeID {
	return atx.NiPosts[0].Posts[0].ID
}

type InitialAtxPartsV2 struct {
	CommitmentATX types.ATXID
	Post          PostV1
	// needed make hash of the first InnerActivationTxV2 unique
	// if the InitialPost happens to be the same for different IDs.
	NodeID types.NodeID
}

// MarriageCertificate proves the will of ID to be married with the ID that includes this certificate.
// A marriage allows for publishing a merged ATX, which can contain PoST for all married IDs.
// Any ID from the marriage can publish a merged ATX on behalf of all married IDs.
type MarriageCertificate struct {
	ID types.NodeID
	// Signature over the other ID that this ID marries with
	// If Alice marries Bob, then Alice signs Bob's ID
	// and Bob includes this certificate in his ATX.
	Signature types.EdSignature
}

// MerkleProofV2 proves membership of multiple challenges in a PoET membership merkle tree.
type MerkleProofV2 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes       []types.Hash32 `scale:"max=32"`
	LeafIndices []uint64       `scale:"max=256"` // support merging up to 256 IDs
}

type SubPostV2 struct {
	ID           types.NodeID // The ID that this PoST is for.
	PrevATXIndex uint32       // Index of the previous ATX in the `InnerActivationTxV2.PreviousATXs` slice
	Post         PostV1
	NumUnits     uint32
}

type NiPostsV2 struct {
	// Single membership proof for all IDs in `Posts`.
	// The index of ID in `Posts` is the index of the challenge in the proof (`LeafIndices`).
	Membership MerkleProofV2
	// The root of the PoET proof, that serves as the challenge for PoSTs.
	Challenge types.Hash32
	Posts     []SubPostV2 `scale:"max=256"` // support merging up to 256 IDs
}
