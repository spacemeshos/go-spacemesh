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
	PreviousATXs PrevATXs `scale:"max=256"`
	NiPosts      NIPosts  `scale:"max=4"`

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

	tree.AddLeaf(atx.PreviousATXs.Root())
	tree.AddLeaf(atx.NiPosts.Root(atx.PreviousATXs))

	var vrfNonce types.Hash32
	binary.LittleEndian.PutUint64(vrfNonce[:], atx.VRFNonce)
	tree.AddLeaf(vrfNonce.Bytes())

	tree.AddLeaf(types.Hash32(atx.Marriages.Root()).Bytes())

	if atx.MarriageATX != nil {
		tree.AddLeaf(atx.MarriageATX.Bytes())
	} else {
		tree.AddLeaf(types.EmptyATXID.Bytes())
	}
}

func (atx *ActivationTxV2) merkleProof(leafIndex MerkleTreeIndex) []types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(leafIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	atx.merkleTree(tree)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

func validateAtxProof(atxID types.ATXID, leaf types.Hash32, proof []types.Hash32, leafIndex MerkleTreeIndex) bool {
	proofBytes := make([][]byte, len(proof))
	for i, h := range proof {
		proofBytes[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(leafIndex)},
		[][]byte{leaf.Bytes()},
		proofBytes,
		atxID.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		panic(err)
	}
	return ok
}

// ID returns the ATX ID. It is the root of the ATX merkle tree.
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

func (atx *ActivationTxV2) PublishEpochProof() []types.Hash32 {
	return atx.merkleProof(PublishEpochIndex)
}

func (atx *ActivationTxV2) PositioningATXProof() []types.Hash32 {
	return atx.merkleProof(PositioningATXIndex)
}

func (atx *ActivationTxV2) CoinbaseProof() []types.Hash32 {
	return atx.merkleProof(CoinbaseIndex)
}

func (atx *ActivationTxV2) InitialPostsRootProof() []types.Hash32 {
	return atx.merkleProof(InitialPostsRootIndex)
}

func (atx *ActivationTxV2) PreviousATXsRootProof() []types.Hash32 {
	return atx.merkleProof(PreviousATXsRootIndex)
}

func (atx *ActivationTxV2) NIPostsRootProof() []types.Hash32 {
	return atx.merkleProof(NIPostsRootIndex)
}

func (atx *ActivationTxV2) VRFNonceProof() []types.Hash32 {
	return atx.merkleProof(VRFNonceIndex)
}

func (atx *ActivationTxV2) MarriagesRootProof() MarriagesRootProof {
	return atx.merkleProof(MarriagesRootIndex)
}

type MarriagesRootProof []types.Hash32

func (p MarriagesRootProof) Valid(atxID types.ATXID, marriagesRoot MarriagesRoot) bool {
	return validateAtxProof(atxID, types.Hash32(marriagesRoot), p, MarriagesRootIndex)
}

func (atx *ActivationTxV2) MarriageATXProof() []types.Hash32 {
	return atx.merkleProof(MarriageATXIndex)
}

type InitialAtxPartsV2 struct {
	CommitmentATX types.ATXID
	Post          PostV1
}

func (parts *InitialAtxPartsV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if parts == nil {
		return nil
	}
	encoder.AddString("CommitmentATX", parts.CommitmentATX.String())
	encoder.AddObject("Post", &parts.Post)
	return nil
}

func (parts *InitialAtxPartsV2) merkleTree(tree *merkle.Tree) {
	tree.AddLeaf(parts.CommitmentATX.Bytes())
	tree.AddLeaf(parts.Post.Root())
}

func (parts *InitialAtxPartsV2) merkleProof(leafIndex InitialPostTreeIndex) []types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(leafIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	parts.merkleTree(tree)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

func (parts *InitialAtxPartsV2) Root() []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	parts.merkleTree(tree)
	return tree.Root()
}

func (parts *InitialAtxPartsV2) CommitmentATXProof() []types.Hash32 {
	return parts.merkleProof(CommitmentATXIndex)
}

func (parts *InitialAtxPartsV2) PostProof() []types.Hash32 {
	return parts.merkleProof(InitialPostIndex)
}

type PrevATXs []types.ATXID

func (prevATXs PrevATXs) merkleTree(tree *merkle.Tree) {
	for _, prevATX := range prevATXs {
		tree.AddLeaf(prevATX.Bytes())
	}
	for i := len(prevATXs); i < 256; i++ {
		tree.AddLeaf(types.EmptyATXID.Bytes())
	}
}

func (prevATXs PrevATXs) Root() []byte {
	prevATXsTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	prevATXs.merkleTree(prevATXsTree)
	return prevATXsTree.Root()
}

type NIPosts []NIPostV2

func (nps NIPosts) merkleTree(tree *merkle.Tree, prevATXs []types.ATXID) {
	for _, niPost := range nps {
		tree.AddLeaf(niPost.Root(prevATXs))
	}
	// Add empty NiPoSTs up to the max scale limit.
	// This must be updated when the max scale limit is changed.
	for i := len(nps); i < 4; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
}

func (nps NIPosts) Root(prevATXs []types.ATXID) []byte {
	niPostTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	nps.merkleTree(niPostTree, prevATXs)
	return niPostTree.Root()
}

func (nps NIPosts) Proof(index int, prevATXs []types.ATXID) []types.Hash32 {
	if index < 0 || index >= len(nps) {
		panic("index out of range")
	}

	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(index): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	nps.merkleTree(tree, prevATXs)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

type NIPostV2 struct {
	// Single membership proof for all IDs in `Posts`.
	Membership MerkleProofV2
	// The root of the PoET proof, that serves as the challenge for PoSTs.
	Challenge types.Hash32
	Posts     SubPostsV2 `scale:"max=256"` // support merging up to 256 IDs
}

func (np *NIPostV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if np == nil {
		return nil
	}
	// skip membership proof
	encoder.AddString("Challenge", np.Challenge.String())
	encoder.AddArray("Posts", zapcore.ArrayMarshalerFunc(func(ae zapcore.ArrayEncoder) error {
		for _, post := range np.Posts {
			ae.AppendObject(&post)
		}
		return nil
	}))
	return nil
}

func (np *NIPostV2) merkleTree(tree *merkle.Tree, prevATXs []types.ATXID) {
	tree.AddLeaf(codec.MustEncode(&np.Membership))
	tree.AddLeaf(np.Challenge.Bytes())
	tree.AddLeaf(np.Posts.Root(prevATXs))
}

func (np *NIPostV2) merkleProof(leafIndex NIPostTreeIndex, prevATXs []types.ATXID) []types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(leafIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	np.merkleTree(tree, prevATXs)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

func (np *NIPostV2) Root(prevATXs []types.ATXID) []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	np.merkleTree(tree, prevATXs)
	return tree.Root()
}

func (np *NIPostV2) MembershipProof(prevATXs []types.ATXID) []types.Hash32 {
	return np.merkleProof(MembershipIndex, prevATXs)
}

func (np *NIPostV2) ChallengeProof(prevATXs []types.ATXID) []types.Hash32 {
	return np.merkleProof(ChallengeIndex, prevATXs)
}

func (np *NIPostV2) PostsRootProof(prevATXs []types.ATXID) []types.Hash32 {
	return np.merkleProof(PostsRootIndex, prevATXs)
}

// MerkleProofV2 proves membership of multiple challenges in a PoET membership merkle tree.
type MerkleProofV2 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes []types.Hash32 `scale:"max=32"`
}

type SubPostsV2 []SubPostV2

func (sp SubPostsV2) merkleTree(tree *merkle.Tree, prevATXs []types.ATXID) {
	for _, subPost := range sp {
		// if root is nil it will be handled like 0x00...00
		// this will still generate a valid ID for the ATX,
		// but syntactical validation will catch the invalid subPost and
		// consider the ATX invalid
		tree.AddLeaf(subPost.Root(prevATXs))
	}
	for i := len(sp); i < 256; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
}

func (sp SubPostsV2) Root(prevATXs []types.ATXID) []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	sp.merkleTree(tree, prevATXs)
	return tree.Root()
}

func (sp SubPostsV2) Proof(index int, prevATXs []types.ATXID) []types.Hash32 {
	if index < 0 || index >= len(sp) {
		panic("index out of range")
	}

	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(index): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	sp.merkleTree(tree, prevATXs)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
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

func (sp *SubPostV2) merkleTree(tree *merkle.Tree, prevATXs []types.ATXID) {
	marriageIndex := make([]byte, 4)
	binary.LittleEndian.PutUint32(marriageIndex, sp.MarriageIndex)
	tree.AddLeaf(marriageIndex)

	if int(sp.PrevATXIndex) >= len(prevATXs) {
		return // invalid index, root cannot be generated
	}
	tree.AddLeaf(prevATXs[sp.PrevATXIndex].Bytes())

	var leafIndex types.Hash32
	binary.LittleEndian.PutUint64(leafIndex[:], sp.MembershipLeafIndex)
	tree.AddLeaf(leafIndex[:])

	tree.AddLeaf(sp.Post.Root())

	numUnits := make([]byte, 4)
	binary.LittleEndian.PutUint32(numUnits, sp.NumUnits)
	tree.AddLeaf(numUnits)
}

func (sp *SubPostV2) merkleProof(leafIndex SubPostTreeIndex, prevATXs []types.ATXID) []types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(leafIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	sp.merkleTree(tree, prevATXs)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

func (sp *SubPostV2) Root(prevATXs []types.ATXID) []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	sp.merkleTree(tree, prevATXs)
	return tree.Root()
}

func (sp *SubPostV2) MarriageIndexProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(MarriageIndex, prevATXs)
}

func (sp *SubPostV2) PrevATXIndexProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(PrevATXIndex, prevATXs)
}

func (sp *SubPostV2) MembershipLeafIndexProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(MembershipLeafIndex, prevATXs)
}

func (sp *SubPostV2) PostProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(PostIndex, prevATXs)
}

func (sp *SubPostV2) NumUnitsProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(NumUnitsIndex, prevATXs)
}

type MarriageCertificates []MarriageCertificate

func (mcs MarriageCertificates) merkleTree(tree *merkle.Tree) {
	for _, marriage := range mcs {
		tree.AddLeaf(marriage.Root())
	}
	for i := len(mcs); i < 256; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
}

type MarriagesRoot types.Hash32

func (mcs MarriageCertificates) Root() MarriagesRoot {
	marriagesTree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	mcs.merkleTree(marriagesTree)
	return MarriagesRoot(marriagesTree.Root())
}

func (mcs MarriageCertificates) Proof(index int) MarriageCertificateProof {
	if index < 0 || index >= len(mcs) {
		panic("index out of range")
	}

	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(index): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	mcs.merkleTree(tree)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

type MarriageCertificateProof []types.Hash32

func (p MarriageCertificateProof) Valid(marriageRoot MarriagesRoot, index int, mc MarriageCertificate) bool {
	proof := make([][]byte, len(p))
	for i, h := range p {
		proof[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{uint64(index)},
		[][]byte{mc.Root()},
		proof,
		types.Hash32(marriageRoot).Bytes(),
		atxTreeHash,
	)
	if err != nil {
		panic(err)
	}
	return ok
}

// MarriageCertificate proves the will of ID to be married with the ID that includes this certificate.
// A marriage allows for publishing a merged ATX, which can contain PoST for all married IDs.
// Any ID from the marriage can publish a merged ATX on behalf of all married IDs.
type MarriageCertificate struct {
	// An ATX of the NodeID that marries. It proves that the NodeID exists.
	// Note: the reference ATX does not need to be from the previous epoch.
	// It only needs to prove the existence of the Identity.
	ReferenceAtx types.ATXID
	// Signature over the other ID that this ID marries with
	// If Alice marries Bob, then Alice signs Bob's ID
	// and Bob includes this certificate in his ATX.
	Signature types.EdSignature
}

func (mc *MarriageCertificate) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if mc == nil {
		return nil
	}
	encoder.AddString("ReferenceATX", mc.ReferenceAtx.String())
	encoder.AddString("Signature", mc.Signature.String())
	return nil
}

func (mc *MarriageCertificate) merkleTree(tree *merkle.Tree) {
	tree.AddLeaf(mc.ReferenceAtx.Bytes())
	tree.AddLeaf(mc.Signature.Bytes())
}

func (mc *MarriageCertificate) merkleProof(leafIndex MarriageCertificateIndex) []types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(leafIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	mc.merkleTree(tree)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

func (mc *MarriageCertificate) Root() []byte {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	mc.merkleTree(tree)
	return tree.Root()
}

func (mc *MarriageCertificate) ReferenceATXProof() []types.Hash32 {
	return mc.merkleProof(ReferenceATXIndex)
}

func (mc *MarriageCertificate) SignatureProof() []types.Hash32 {
	return mc.merkleProof(SignatureIndex)
}

func atxTreeHash(buf, lChild, rChild []byte) []byte {
	hash := blake3.New()
	hash.Write([]byte{0x01})
	hash.Write(lChild)
	hash.Write(rChild)
	return hash.Sum(buf)
}
