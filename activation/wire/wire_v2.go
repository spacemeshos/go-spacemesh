package wire

import (
	"unsafe"

	"github.com/spacemeshos/merkle-tree"
	"github.com/zeebo/blake3"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

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

	SmesherID types.NodeID
	Signature types.EdSignature

	// cached fields to avoid repeated calculations
	id types.ATXID
}

func (atx *ActivationTxV2) SignedBytes() []byte {
	return atx.ID().Bytes()
}

func (atx *ActivationTxV2) ID() types.ATXID {
	if atx.id != types.EmptyATXID {
		return atx.id
	}

	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}

	tree.AddLeaf((*[4]byte)(unsafe.Pointer(&atx.PublishEpoch))[:])
	tree.AddLeaf(atx.PositioningATX.Bytes())
	if atx.Coinbase != nil {
		tree.AddLeaf(atx.Coinbase.Bytes())
	} else {
		tree.AddLeaf(types.Address{}.Bytes())
	}
	if atx.Initial != nil {
		tree.AddLeaf(atx.Initial.Root())
	} else {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}

	prevATXTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	for _, prevATX := range atx.PreviousATXs {
		prevATXTree.AddLeaf(prevATX.Bytes())
	}
	for i := 0; i < 256-len(atx.PreviousATXs); i++ {
		prevATXTree.AddLeaf(types.EmptyATXID.Bytes())
	}
	tree.AddLeaf(prevATXTree.Root())

	niPostTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	for _, niPost := range atx.NiPosts {
		niPostTree.AddLeaf(niPost.Root(atx.PreviousATXs))
	}
	for i := 0; i < 2-len(atx.NiPosts); i++ {
		niPostTree.AddLeaf(types.EmptyHash32.Bytes())
	}
	tree.AddLeaf(niPostTree.Root())

	if atx.VRFNonce != nil {
		tree.AddLeaf((*[8]byte)(unsafe.Pointer(atx.VRFNonce))[:])
	} else {
		tree.AddLeaf([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	}

	marriagesTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	for _, marriage := range atx.Marriages {
		marriagesTree.AddLeaf(marriage.Root())
	}
	for i := 0; i < 256-len(atx.Marriages); i++ {
		marriagesTree.AddLeaf(types.EmptyHash32.Bytes())
	}
	tree.AddLeaf(marriagesTree.Root())

	if atx.MarriageATX != nil {
		tree.AddLeaf(atx.MarriageATX.Bytes())
	} else {
		tree.AddLeaf(types.EmptyATXID.Bytes())
	}

	atx.id = types.ATXID(tree.Root())
	return atx.id
}

func (atx *ActivationTxV2) Sign(signer *signing.EdSigner) {
	atx.SmesherID = signer.NodeID()
	atx.Signature = signer.Sign(signing.ATX, atx.SignedBytes())
}

type InitialAtxPartsV2 struct {
	CommitmentATX types.ATXID
	Post          PostV1
}

func (i *InitialAtxPartsV2) Root() []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	tree.AddLeaf(i.CommitmentATX.Bytes())
	tree.AddLeaf(i.Post.Root())
	return tree.Root()
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

func (mc *MarriageCertificate) Root() []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	tree.AddLeaf(mc.ID.Bytes())
	tree.AddLeaf(mc.Signature.Bytes())
	return tree.Root()
}

// MerkleProofV2 proves membership of multiple challenges in a PoET membership merkle tree.
type MerkleProofV2 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes       []types.Hash32 `scale:"max=32"`
	LeafIndices []uint64       `scale:"max=256"` // support merging up to 256 IDs
}

func (mp *MerkleProofV2) Root() []byte {
	nodeTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	for _, node := range mp.Nodes {
		nodeTree.AddLeaf(node.Bytes())
	}
	for i := 0; i < 32-len(mp.Nodes); i++ {
		nodeTree.AddLeaf(types.EmptyHash32.Bytes())
	}
	leafIndicesTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	for _, index := range mp.LeafIndices {
		leafIndicesTree.AddLeaf((*[8]byte)(unsafe.Pointer(&index))[:])
	}
	for i := 0; i < 256-len(mp.LeafIndices); i++ {
		leafIndicesTree.AddLeaf([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	}

	merkleTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	merkleTree.AddLeaf(nodeTree.Root())
	merkleTree.AddLeaf(leafIndicesTree.Root())
	return merkleTree.Root()
}

type SubPostV2 struct {
	NodeID       types.NodeID // NodeID this PoST belongs to
	PrevATXIndex uint32       // Index of the previous ATX in the `InnerActivationTxV2.PreviousATXs` slice
	Post         PostV1
	NumUnits     uint32
}

func (sp *SubPostV2) Root(prevATXs []types.ATXID) []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	tree.AddLeaf(sp.NodeID.Bytes())
	if int(sp.PrevATXIndex) >= len(prevATXs) {
		return nil // invalid index, root cannot be generated
	}
	tree.AddLeaf(prevATXs[sp.PrevATXIndex].Bytes())
	tree.AddLeaf(sp.Post.Root())
	tree.AddLeaf((*[4]byte)(unsafe.Pointer(&sp.NumUnits))[:])
	return tree.Root()
}

type NiPostsV2 struct {
	// Single membership proof for all IDs in `Posts`.
	// The index of ID in `Posts` is the index of the challenge in the proof (`LeafIndices`).
	Membership MerkleProofV2
	// The root of the PoET proof, that serves as the challenge for PoSTs.
	Challenge types.Hash32
	Posts     []SubPostV2 `scale:"max=256"` // support merging up to 256 IDs
}

func (np *NiPostsV2) Root(prevATXs []types.ATXID) []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	tree.AddLeaf(np.Membership.Root())
	tree.AddLeaf(np.Challenge.Bytes())

	postsTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	for _, subPost := range np.Posts {
		// if root is nil it will be handled like 0x00...00
		// this will still generate a valid ID for the ATX,
		// but syntactical validation will catch the invalid subPost and
		// consider the ATX invalid
		postsTree.AddLeaf(subPost.Root(prevATXs))
	}
	for i := 0; i < 256-len(np.Posts); i++ {
		postsTree.AddLeaf(types.EmptyHash32.Bytes())
	}
	tree.AddLeaf(postsTree.Root())
	return tree.Root()
}

func atxTreeHash(buf, lChild, rChild []byte) []byte {
	hash := blake3.New()
	hash.Write([]byte{0x01})
	hash.Write(lChild)
	hash.Write(rChild)
	return hash.Sum(buf)
}
