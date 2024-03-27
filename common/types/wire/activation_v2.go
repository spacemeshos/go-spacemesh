package wire

//go:generate scalegen

type ActivationTxV2 struct {
	InnerActivationTxV2

	SmesherID [32]byte
	Signature [64]byte
}

type InitialAtxPartsV2 struct {
	CommitmentATX Hash32
	InitialPost   PostV2
	NodeID        Hash32 // needed make hash of the first InnerActivationTxV2 unique
}

type InnerActivationTxV2 struct {
	PublishEpoch uint32

	PrevATXID      [32]byte
	PositioningATX [32]byte

	// only present in initial ATX
	Initial *InitialAtxPartsV2

	Coinbase Address

	NiPosts NiPostsV2

	// The primary ID that this ID merges into.
	// This ID is supposed to take over smeshing for the delegated ID.
	// The primary ID must include ATXID of this ATX in its next ATX in the `DelegatingATXs` field
	// in order to complete delegation (ATX merge).
	DelegateTo [32]byte
	// The list of ATXs of IDs that this ID takes over (merges with).
	// Also used as the previous ATX to reconstruct the nipost challenge
	// for IDs that are merged into this one (only once). Subsequent
	// merged ATXs have 1 nipost challenge and use the `PrevATXID` field.
	DelegatingATXs []Hash32 `scale:"max=1000"`
}

// MerkleProofV2 proves membership of multiple challenges in a PoET membership merkle tree.
type MerkleProofV2 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes       []Hash32 `scale:"max=32"`
	LeafIndices []uint64 `scale:"max=1000"` // support merging up to 1k IDs
}

type PostV2 struct {
	Nonce   uint32
	Indices []byte `scale:"max=800"` // up to K2=100
	Pow     uint64
}

type NiPostsV2 struct {
	// Single membership proof for all IDs in `Posts`.
	// The index of ID in `Posts` is the index of the challenge in the proof (`LeafIndices`).
	Membership MerkleProofV2
	Challenge  []byte `scale:"max=32"`
	Posts      []struct {
		NumUnits uint32
		Post     PostV2
		// NodeID of the ID that this PoST is for.
		Id       Hash32
		VRFNonce *VRFPostIndex // only present when the nonce changed (or initial ATX)
	} `scale:"max=1000"` // support merging up to 1k IDs
}
