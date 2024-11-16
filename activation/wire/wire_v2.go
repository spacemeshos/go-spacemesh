package wire

import (
	"encoding/binary"

	"github.com/spacemeshos/merkle-tree"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
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
	NIPosts      NIPosts  `scale:"max=4"`

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
	for _, post := range atx.NIPosts {
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
		for _, nipost := range atx.NIPosts {
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

	var coinbase types.Hash32
	copy(coinbase[:], atx.Coinbase.Bytes())
	tree.AddLeaf(coinbase.Bytes())

	if atx.Initial != nil {
		tree.AddLeaf(types.Hash32(atx.Initial.Root()).Bytes())
	} else {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}

	tree.AddLeaf(types.Hash32(atx.PreviousATXs.Root()).Bytes())
	tree.AddLeaf(types.Hash32(atx.NIPosts.Root(atx.PreviousATXs)).Bytes())

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
	return createProof(uint64(leafIndex), atx.merkleTree)
}

// ID returns the ATX ID. It is the root of the ATX merkle tree.
func (atx *ActivationTxV2) ID() types.ATXID {
	if atx.id != types.EmptyATXID {
		return atx.id
	}
	atx.id = types.ATXID(createRoot(atx.merkleTree))
	return atx.id
}

func (atx *ActivationTxV2) PublishEpochProof() PublishEpochProof {
	return atx.merkleProof(PublishEpochIndex)
}

type PublishEpochProof []types.Hash32

func (p PublishEpochProof) Valid(atxID types.ATXID, publishEpoch types.EpochID) bool {
	var publishEpochBytes types.Hash32
	binary.LittleEndian.PutUint32(publishEpochBytes[:], publishEpoch.Uint32())
	return validateProof(types.Hash32(atxID), publishEpochBytes, p, uint64(PublishEpochIndex))
}

func (atx *ActivationTxV2) PositioningATXProof() []types.Hash32 {
	return atx.merkleProof(PositioningATXIndex)
}

func (atx *ActivationTxV2) CoinbaseProof() []types.Hash32 {
	return atx.merkleProof(CoinbaseIndex)
}

func (atx *ActivationTxV2) InitialPostRootProof() InitialPostRootProof {
	return atx.merkleProof(InitialPostRootIndex)
}

type InitialPostRootProof []types.Hash32

func (p InitialPostRootProof) Valid(atxID types.ATXID, initialPostRoot InitialPostRoot) bool {
	return validateProof(types.Hash32(atxID), types.Hash32(initialPostRoot), p, uint64(InitialPostRootIndex))
}

func (atx *ActivationTxV2) PreviousATXsRootProof() PrevATXsRootProof {
	return atx.merkleProof(PreviousATXsRootIndex)
}

type PrevATXsRootProof []types.Hash32

func (p PrevATXsRootProof) Valid(atxID types.ATXID, prevATXsRoot PrevATXsRoot) bool {
	return validateProof(types.Hash32(atxID), types.Hash32(prevATXsRoot), p, uint64(PreviousATXsRootIndex))
}

func (atx *ActivationTxV2) NIPostsRootProof() NIPostsRootProof {
	return atx.merkleProof(NIPostsRootIndex)
}

type NIPostsRootProof []types.Hash32

func (p NIPostsRootProof) Valid(atxID types.ATXID, niPostsRoot NIPostsRoot) bool {
	return validateProof(types.Hash32(atxID), types.Hash32(niPostsRoot), p, uint64(NIPostsRootIndex))
}

func (atx *ActivationTxV2) VRFNonceProof() []types.Hash32 {
	return atx.merkleProof(VRFNonceIndex)
}

func (atx *ActivationTxV2) MarriagesRootProof() MarriageCertificatesRootProof {
	return atx.merkleProof(MarriagesRootIndex)
}

type MarriageCertificatesRootProof []types.Hash32

func (p MarriageCertificatesRootProof) Valid(atxID types.ATXID, marriagesRoot MarriageCertificatesRoot) bool {
	return validateProof(types.Hash32(atxID), types.Hash32(marriagesRoot), p, uint64(MarriagesRootIndex))
}

func (atx *ActivationTxV2) MarriageATXProof() MarriageATXProof {
	return atx.merkleProof(MarriageATXIndex)
}

type MarriageATXProof []types.Hash32

func (p MarriageATXProof) Valid(atxID, marriageATX types.ATXID) bool {
	return validateProof(types.Hash32(atxID), types.Hash32(marriageATX), p, uint64(MarriageATXIndex))
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
	tree.AddLeaf(types.Hash32(parts.Post.Root()).Bytes())
}

func (parts *InitialAtxPartsV2) merkleProof(leafIndex InitialPostTreeIndex) []types.Hash32 {
	return createProof(uint64(leafIndex), parts.merkleTree)
}

type InitialPostRoot types.Hash32

func (parts *InitialAtxPartsV2) Root() InitialPostRoot {
	return InitialPostRoot(createRoot(parts.merkleTree))
}

func (parts *InitialAtxPartsV2) CommitmentATXProof() CommitmentATXProof {
	return parts.merkleProof(CommitmentATXIndex)
}

type CommitmentATXProof []types.Hash32

func (p CommitmentATXProof) Valid(initialPostRoot InitialPostRoot, commitmentATX types.ATXID) bool {
	return validateProof(types.Hash32(initialPostRoot), types.Hash32(commitmentATX), p, uint64(CommitmentATXIndex))
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

type PrevATXsRoot types.Hash32

func (prevATXs PrevATXs) Root() PrevATXsRoot {
	return PrevATXsRoot(createRoot(prevATXs.merkleTree))
}

func (prevATXs PrevATXs) Proof(index int) PrevATXsProof {
	if index < 0 || index >= len(prevATXs) {
		panic("index out of range")
	}
	return createProof(uint64(index), prevATXs.merkleTree)
}

type PrevATXsProof []types.Hash32

func (p PrevATXsProof) Valid(prevATXsRoot PrevATXsRoot, index int, prevATX types.ATXID) bool {
	return validateProof(types.Hash32(prevATXsRoot), types.Hash32(prevATX), p, uint64(index))
}

type NIPosts []NIPostV2

func (nps NIPosts) merkleTree(tree *merkle.Tree, prevATXs []types.ATXID) {
	for _, niPost := range nps {
		tree.AddLeaf(types.Hash32(niPost.Root(prevATXs)).Bytes())
	}
	// Add empty NiPoSTs up to the max scale limit.
	// This must be updated when the max scale limit is changed.
	for i := len(nps); i < 4; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
}

type NIPostsRoot types.Hash32

func (nps NIPosts) Root(prevATXs []types.ATXID) NIPostsRoot {
	return NIPostsRoot(createRoot(func(tree *merkle.Tree) {
		nps.merkleTree(tree, prevATXs)
	}))
}

func (nps NIPosts) Proof(index int, prevATXs []types.ATXID) NIPostRootProof {
	if index < 0 || index >= len(nps) {
		panic("index out of range")
	}
	return createProof(uint64(index), func(tree *merkle.Tree) {
		nps.merkleTree(tree, prevATXs)
	})
}

type NIPostRootProof []types.Hash32

func (p NIPostRootProof) Valid(niPostsRoot NIPostsRoot, index int, nipostRoot NIPostRoot) bool {
	return validateProof(types.Hash32(niPostsRoot), types.Hash32(nipostRoot), p, uint64(index))
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
	tree.AddLeaf(np.Membership.Root().Bytes())
	tree.AddLeaf(np.Challenge.Bytes())
	tree.AddLeaf(types.Hash32(np.Posts.Root(prevATXs)).Bytes())
}

func (np *NIPostV2) merkleProof(leafIndex NIPostTreeIndex, prevATXs []types.ATXID) []types.Hash32 {
	return createProof(uint64(leafIndex), func(tree *merkle.Tree) {
		np.merkleTree(tree, prevATXs)
	})
}

type NIPostRoot types.Hash32

func (np *NIPostV2) Root(prevATXs []types.ATXID) NIPostRoot {
	return NIPostRoot(createRoot(func(tree *merkle.Tree) {
		np.merkleTree(tree, prevATXs)
	}))
}

func (np *NIPostV2) MembershipProof(prevATXs []types.ATXID) []types.Hash32 {
	return np.merkleProof(MembershipIndex, prevATXs)
}

func (np *NIPostV2) ChallengeProof(prevATXs []types.ATXID) []types.Hash32 {
	return np.merkleProof(ChallengeIndex, prevATXs)
}

type ChallengeProof []types.Hash32

func (p ChallengeProof) Valid(nipostRoot NIPostRoot, challenge types.Hash32) bool {
	return validateProof(types.Hash32(nipostRoot), challenge, p, uint64(ChallengeIndex))
}

func (np *NIPostV2) PostsRootProof(prevATXs []types.ATXID) SubPostsRootProof {
	return np.merkleProof(PostsRootIndex, prevATXs)
}

type SubPostsRootProof []types.Hash32

func (p SubPostsRootProof) Valid(nipostRoot NIPostRoot, postsRoot SubPostsRoot) bool {
	return validateProof(types.Hash32(nipostRoot), types.Hash32(postsRoot), p, uint64(PostsRootIndex))
}

// MerkleProofV2 proves membership of multiple challenges in a PoET membership merkle tree.
type MerkleProofV2 struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes []types.Hash32 `scale:"max=32"`
}

func (mp *MerkleProofV2) Root() (result types.Hash32) {
	h := hash.GetHasher()
	defer hash.PutHasher(h)
	codec.MustEncodeTo(h, mp)
	h.Sum(result[:0])
	return result
}

type SubPostsV2 []SubPostV2

func (sp SubPostsV2) merkleTree(tree *merkle.Tree, prevATXs []types.ATXID) {
	for _, subPost := range sp {
		// if root is nil it will be handled like 0x00...00
		// this will still generate a valid ID for the ATX,
		// but syntactical validation will catch the invalid subPost and
		// consider the ATX invalid
		tree.AddLeaf(types.Hash32(subPost.Root(prevATXs)).Bytes())
	}
	for i := len(sp); i < 256; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
}

type SubPostsRoot types.Hash32

func (sp SubPostsV2) Root(prevATXs []types.ATXID) SubPostsRoot {
	return SubPostsRoot(createRoot(func(tree *merkle.Tree) {
		sp.merkleTree(tree, prevATXs)
	}))
}

func (sp SubPostsV2) Proof(index int, prevATXs []types.ATXID) SubPostRootProof {
	if index < 0 || index >= len(sp) {
		panic("index out of range")
	}
	return createProof(uint64(index), func(tree *merkle.Tree) {
		sp.merkleTree(tree, prevATXs)
	})
}

type SubPostRootProof []types.Hash32

func (p SubPostRootProof) Valid(subPostsRoot SubPostsRoot, index int, subPostRoot SubPostRoot) bool {
	return validateProof(types.Hash32(subPostsRoot), types.Hash32(subPostRoot), p, uint64(index))
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

func (sp *SubPostV2) merkleTree(tree *merkle.Tree, prevATX types.ATXID) {
	var marriageIndex types.Hash32
	binary.LittleEndian.PutUint32(marriageIndex[:], sp.MarriageIndex)
	tree.AddLeaf(marriageIndex.Bytes())

	tree.AddLeaf(prevATX.Bytes())

	var leafIndex types.Hash32
	binary.LittleEndian.PutUint64(leafIndex[:], sp.MembershipLeafIndex)
	tree.AddLeaf(leafIndex[:])

	tree.AddLeaf(types.Hash32(sp.Post.Root()).Bytes())

	var numUnits types.Hash32
	binary.LittleEndian.PutUint32(numUnits[:], sp.NumUnits)
	tree.AddLeaf(numUnits.Bytes())
}

func (sp *SubPostV2) merkleProof(leafIndex SubPostTreeIndex, prevATXs []types.ATXID) []types.Hash32 {
	return createProof(uint64(leafIndex), func(tree *merkle.Tree) {
		var prevATX types.ATXID
		switch {
		case len(prevATXs) == 0: // special case for initial ATX: prevATXs is empty
			prevATX = types.EmptyATXID
		case int(sp.PrevATXIndex) < len(prevATXs):
			prevATX = prevATXs[sp.PrevATXIndex]
		default:
			// not the full set of prevATXs is provided, proof cannot be generated
			panic("prevATXIndex out of range or prevATXs incomplete")
		}
		sp.merkleTree(tree, prevATX)
	})
}

type SubPostRoot types.Hash32

func (sp *SubPostV2) Root(prevATXs []types.ATXID) SubPostRoot {
	return SubPostRoot(createRoot(func(tree *merkle.Tree) {
		var prevATX types.ATXID
		switch {
		case len(prevATXs) == 0: // special case for initial ATX: prevATXs is empty
			prevATX = types.EmptyATXID
		case int(sp.PrevATXIndex) < len(prevATXs):
			prevATX = prevATXs[sp.PrevATXIndex]
		default:
			// prevATXIndex is out of range, don't fail ATXID generation
			// will be detected by syntactical validation
			prevATX = types.EmptyATXID
		}
		sp.merkleTree(tree, prevATX)
	}))
}

func (sp *SubPostV2) MarriageIndexProof(prevATXs []types.ATXID) MarriageIndexProof {
	return sp.merkleProof(MarriageIndex, prevATXs)
}

type MarriageIndexProof []types.Hash32

func (p MarriageIndexProof) Valid(subPostRoot SubPostRoot, marriageIndex uint32) bool {
	var marriageIndexBytes types.Hash32
	binary.LittleEndian.PutUint32(marriageIndexBytes[:], marriageIndex)
	return validateProof(types.Hash32(subPostRoot), marriageIndexBytes, p, uint64(MarriageIndex))
}

func (sp *SubPostV2) PrevATXIndexProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(PrevATXIndex, prevATXs)
}

func (sp *SubPostV2) PrevATXProof(prevATX types.ATXID) PrevATXProof {
	return createProof(uint64(SubPostTreeIndex(PrevATXIndex)), func(tree *merkle.Tree) {
		sp.merkleTree(tree, prevATX)
	})
}

type PrevATXProof []types.Hash32

func (p PrevATXProof) Valid(subPostRoot SubPostRoot, prevATX types.ATXID) bool {
	return validateProof(types.Hash32(subPostRoot), types.Hash32(prevATX), p, uint64(PrevATXIndex))
}

func (sp *SubPostV2) MembershipLeafIndexProof(prevATXs []types.ATXID) []types.Hash32 {
	return sp.merkleProof(MembershipLeafIndex, prevATXs)
}

func (sp *SubPostV2) PostProof(prevATXs []types.ATXID) PostRootProof {
	return sp.merkleProof(PostIndex, prevATXs)
}

type PostRootProof []types.Hash32

func (p PostRootProof) Valid(subPostRoot SubPostRoot, postRoot PostRoot) bool {
	return validateProof(types.Hash32(subPostRoot), types.Hash32(postRoot), p, uint64(PostIndex))
}

func (sp *SubPostV2) NumUnitsProof(prevATXs []types.ATXID) NumUnitsProof {
	return sp.merkleProof(NumUnitsIndex, prevATXs)
}

type NumUnitsProof []types.Hash32

func (p NumUnitsProof) Valid(subPostRoot SubPostRoot, numUnits uint32) bool {
	var numUnitsBytes types.Hash32
	binary.LittleEndian.PutUint32(numUnitsBytes[:], numUnits)
	return validateProof(types.Hash32(subPostRoot), numUnitsBytes, p, uint64(NumUnitsIndex))
}

type MarriageCertificates []MarriageCertificate

func (mcs MarriageCertificates) merkleTree(tree *merkle.Tree) {
	for _, marriage := range mcs {
		tree.AddLeaf(marriage.Root().Bytes())
	}
	for i := len(mcs); i < 256; i++ {
		tree.AddLeaf(types.EmptyHash32.Bytes())
	}
}

type MarriageCertificatesRoot types.Hash32

func (mcs MarriageCertificates) Root() MarriageCertificatesRoot {
	return MarriageCertificatesRoot(createRoot(mcs.merkleTree))
}

func (mcs MarriageCertificates) Proof(index int) MarriageCertificateProof {
	if index < 0 || index >= len(mcs) {
		panic("index out of range")
	}
	return createProof(uint64(index), mcs.merkleTree)
}

type MarriageCertificateProof []types.Hash32

func (p MarriageCertificateProof) Valid(marriageRoot MarriageCertificatesRoot, index int, mc MarriageCertificate) bool {
	return validateProof(types.Hash32(marriageRoot), types.Hash32(mc.Root()), p, uint64(index))
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

func (mc *MarriageCertificate) Root() (result types.Hash32) {
	h := hash.GetHasher()
	defer hash.PutHasher(h)
	codec.MustEncodeTo(h, mc)
	h.Sum(result[:0])
	return result
}

func atxTreeHash(buf, lChild, rChild []byte) []byte {
	h := hash.GetHasher()
	defer hash.PutHasher(h)
	h.Write([]byte{0x01})
	h.Write(lChild)
	h.Write(rChild)
	return h.Sum(buf)
}

func createRoot(addLeaves func(tree *merkle.Tree)) types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	addLeaves(tree)
	return types.Hash32(tree.Root())
}

func createProof(leafIndex uint64, addLeaves func(tree *merkle.Tree)) []types.Hash32 {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(leafIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		panic(err)
	}
	addLeaves(tree)
	proof := tree.Proof()
	proofHashes := make([]types.Hash32, len(proof))
	for i, p := range proof {
		proofHashes[i] = types.Hash32(p)
	}
	return proofHashes
}

func validateProof(root, leaf types.Hash32, proof []types.Hash32, leafIndex uint64) bool {
	proofBytes := make([][]byte, len(proof))
	for i, h := range proof {
		proofBytes[i] = h.Bytes()
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{leafIndex},
		[][]byte{leaf.Bytes()},
		proofBytes,
		root.Bytes(),
		atxTreeHash,
	)
	if err != nil {
		panic(err)
	}
	return ok
}
