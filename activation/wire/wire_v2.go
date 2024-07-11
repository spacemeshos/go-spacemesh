package wire

import (
	"encoding/binary"

	"github.com/spacemeshos/merkle-tree"
	"github.com/zeebo/blake3"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

type ActivationTxV2 struct {
	PublishEpoch   types.EpochID
	PositioningATX types.ATXID
	Coinbase       types.Address

	// only present in initial ATX
	Initial      *InitialAtxPartsV2
	PreviousATXs []types.ATXID `scale:"max=256"`
	NiPosts      []NiPostsV2   `scale:"max=2"`

	// The VRF nonce must be valid for the collected space of all included IDs.
	VRFNonce uint64

	// The list of marriages with other IDs.
	// A marriage is permanent and cannot be revoked or repeated.
	// All new IDs that are married to this ID are added to the equivocation set
	// that this ID belongs to.
	// It must contain a self-marriage certificate (needed for malfeasance proofs).
	Marriages MarriageCertificates `scale:"max=256"`

	// The ID of the ATX containing marriage for the included IDs.
	// Only required when the ATX includes married IDs.
	MarriageATX *types.ATXID

	SmesherID types.NodeID
	Signature types.EdSignature

	// cached fields to avoid repeated calculations
	id   types.ATXID
	blob []byte
}

func (atx *ActivationTxV2) Blob() types.AtxBlob {
	if len(atx.blob) == 0 {
		atx.blob = codec.MustEncode(atx)
	}
	return types.AtxBlob{
		Blob:    atx.blob,
		Version: types.AtxV2,
	}
}

func DecodeAtxV2(blob []byte) (*ActivationTxV2, error) {
	atx := &ActivationTxV2{
		blob: blob,
	}
	if err := codec.Decode(blob, atx); err != nil {
		return nil, err
	}
	return atx, nil
}

func (atx *ActivationTxV2) merkleTree(tree *merkle.Tree) {
	var publishEpoch types.Hash32
	binary.LittleEndian.PutUint32(publishEpoch[:], atx.PublishEpoch.Uint32())
	tree.AddLeaf(publishEpoch.Bytes())
	tree.AddLeaf(atx.PositioningATX.Bytes())
	tree.AddLeaf(atx.Coinbase.Bytes())

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
	for i := len(atx.PreviousATXs); i < 256; i++ {
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
	for i := len(atx.NiPosts); i < 2; i++ {
		niPostTree.AddLeaf(types.EmptyHash32.Bytes())
	}
	tree.AddLeaf(niPostTree.Root())

	var vrfNonce types.Hash32
	binary.LittleEndian.PutUint64(vrfNonce[:], atx.VRFNonce)
	tree.AddLeaf(vrfNonce.Bytes())

	tree.AddLeaf(atx.Marriages.Root())

	if atx.MarriageATX != nil {
		tree.AddLeaf(atx.MarriageATX.Bytes())
	} else {
		tree.AddLeaf(types.EmptyATXID.Bytes())
	}
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
	atx.merkleTree(tree)
	atx.id = types.ATXID(tree.Root())
	return atx.id
}

func (atx *ActivationTxV2) Sign(signer *signing.EdSigner) {
	atx.SmesherID = signer.NodeID()
	atx.Signature = signer.Sign(signing.ATX, atx.ID().Bytes())
}

func (atx *ActivationTxV2) TotalNumUnits() uint32 {
	var total uint32
	for _, post := range atx.NiPosts {
		for _, subPost := range post.Posts {
			total += subPost.NumUnits
		}
	}
	return total
}

type MarriageCertificates []MarriageCertificate

func (mcs MarriageCertificates) Root() []byte {
	marriagesTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	mcs.merkleTree(marriagesTree)
	return marriagesTree.Root()
}

func (mcs MarriageCertificates) merkleTree(tree *merkle.Tree) {
	for _, marriage := range mcs {
		tree.AddLeaf(marriage.Root())
	}
	for i := len(mcs); i < 256; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
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
	// An ATX of the ID that marries. It proves that the ID exists.
	// Note: the reference ATX does not need to be from the previous epoch.
	// It only needs to prove the existence of the ID.
	//
	// In the case of a self signed certificate that is included in the Marriage ATX by the Smesher signing the ATX,
	// this can be `types.EmptyATXID`.
	ReferenceAtx types.ATXID
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
	tree.AddLeaf(mc.ReferenceAtx.Bytes())
	tree.AddLeaf(mc.Signature.Bytes())
	return tree.Root()
}

// MerkleProofV2 proves membership of multiple challenges in a PoET membership merkle tree.
type MerkleProofV2 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes []types.Hash32 `scale:"max=32"`
}

type SubPostV2 struct {
	// Index of marriage certificate for this ID in the 'Marriages' slice. Only valid for merged ATXs.
	// Can be used to extract the nodeID and verify if it is married with the smesher of the ATX.
	// Must be 0 for non-merged ATXs.
	MarriageIndex uint32
	PrevATXIndex  uint32 // Index of the previous ATX in the `InnerActivationTxV2.PreviousATXs` slice
	// Index of the leaf for this ID's challenge in the poet membership tree.
	// IDs might shared the same index if their nipost challenges are equal.
	// This happens when the IDs are continuously merged (they share the previous ATX).
	MembershipLeafIndex uint64
	Post                PostV1
	NumUnits            uint32
}

func (sp *SubPostV2) Root(prevATXs []types.ATXID) []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	var marriageIndex types.Hash32
	binary.LittleEndian.PutUint32(marriageIndex[:], sp.MarriageIndex)
	tree.AddLeaf(marriageIndex.Bytes())

	if int(sp.PrevATXIndex) >= len(prevATXs) {
		return nil // invalid index, root cannot be generated
	}
	tree.AddLeaf(prevATXs[sp.PrevATXIndex].Bytes())

	var leafIndex types.Hash32
	binary.LittleEndian.PutUint64(leafIndex[:], sp.MembershipLeafIndex)
	tree.AddLeaf(leafIndex[:])

	tree.AddLeaf(sp.Post.Root())

	var numUnits types.Hash32
	binary.LittleEndian.PutUint32(numUnits[:], sp.NumUnits)
	tree.AddLeaf(numUnits.Bytes())
	return tree.Root()
}

type NiPostsV2 struct {
	// Single membership proof for all IDs in `Posts`.
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
	tree.AddLeaf(codec.MustEncode(&np.Membership))
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
	for i := len(np.Posts); i < 256; i++ {
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

func (atx *ActivationTxV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if atx == nil {
		return nil
	}
	encoder.AddString("ID", atx.ID().String())
	encoder.AddString("Smesher", atx.SmesherID.String())
	encoder.AddUint32("PublishEpoch", atx.PublishEpoch.Uint32())
	encoder.AddString("PositioningATX", atx.PositioningATX.String())
	encoder.AddString("Coinbase", atx.Coinbase.String())
	encoder.AddObject("Initial", atx.Initial)
	encoder.AddArray("PreviousATXs", types.ATXIDs(atx.PreviousATXs))
	encoder.AddArray("NiPosts", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, nipost := range atx.NiPosts {
			encoder.AppendObject(&nipost)
		}
		return nil
	}))
	encoder.AddUint64("VRFNonce", atx.VRFNonce)

	encoder.AddArray("Marriages", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, marriage := range atx.Marriages {
			encoder.AppendObject(&marriage)
		}
		return nil
	}))
	if atx.MarriageATX != nil {
		encoder.AddString("MarriageATX", atx.MarriageATX.String())
	}
	encoder.AddString("Signature", atx.Signature.String())
	return nil
}

func (marriage *MarriageCertificate) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if marriage == nil {
		return nil
	}
	encoder.AddString("ReferenceATX", marriage.ReferenceAtx.String())
	encoder.AddString("Signature", marriage.Signature.String())
	return nil
}

func (parts *InitialAtxPartsV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if parts == nil {
		return nil
	}
	encoder.AddString("CommitmentATX", parts.CommitmentATX.String())
	encoder.AddObject("Post", &parts.Post)
	return nil
}

func (post *SubPostV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if post == nil {
		return nil
	}
	encoder.AddUint32("MarriageIndex", post.MarriageIndex)
	encoder.AddUint32("PrevATXIndex", post.PrevATXIndex)
	encoder.AddUint64("MembershipLeafIndex", post.MembershipLeafIndex)
	encoder.AddObject("Post", &post.Post)
	encoder.AddUint32("NumUnits", post.NumUnits)
	return nil
}

func (posts *NiPostsV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if posts == nil {
		return nil
	}
	// skip membership proof
	encoder.AddString("Challenge", posts.Challenge.String())
	encoder.AddArray("Posts", zapcore.ArrayMarshalerFunc(func(ae zapcore.ArrayEncoder) error {
		for _, post := range posts.Posts {
			ae.AppendObject(&post)
		}
		return nil
	}))
	return nil
}
