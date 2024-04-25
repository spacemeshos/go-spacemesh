package wire

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

type ActivationTxV2 struct {
	InnerActivationTxV2

	SmesherID types.NodeID
	// Signature over InnerActivationTxV2
	Signature types.EdSignature
}

type InitialAtxPartsV2 struct {
	CommitmentATX types.Hash32
	InitialPost   PostV1
	// needed make hash of the first InnerActivationTxV2 unique
	// if the InitialPost happens to be the same for different IDs.
	NodeID types.NodeID
}

type InnerActivationTxV2 struct {
	PublishEpoch   uint32
	Coinbase       types.Address
	PositioningATX types.ATXID

	// only present in initial ATX
	Initial      *InitialAtxPartsV2
	PreviousATXs []types.ATXID `scale:"max=100"`
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
	Marriages []MarriageCertificate `scale:"max=100"`
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
	LeafIndices []uint64       `scale:"max=100"` // support merging up to 100 IDs
}

type SubPostV2 struct {
	ID           types.NodeID // Delegating ID that this PoST is for.
	PrevATXIndex uint         // Index of the previous ATX in the `InnerActivationTxV2.PreviousATXs` slice
	Post         PostV1
	NumUnits     uint32
}

type NiPostsV2 struct {
	// Single membership proof for all IDs in `Posts`.
	// The index of ID in `Posts` is the index of the challenge in the proof (`LeafIndices`).
	Membership MerkleProofV2
	// The root of the PoET proof, that serves as the challenge for PoSTs.
	Challenge types.Hash32
	Posts     []SubPostV2 `scale:"max=100"` // support merging up to 100 IDs
}
