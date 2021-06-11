// Package types defines the types used by go-spacemesh consensus algorithms and structs
package types

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// BlockID is a 20-byte sha256 sum of the serialized block, used to identify it.
type BlockID Hash20

// String returns a short prefix of the hex representation of the ID.
func (id BlockID) String() string {
	return id.AsHash32().ShortString()
}

// Field returns a log field. Implements the LoggableField interface.
func (id BlockID) Field() log.Field {
	return log.String("block_id", id.AsHash32().ShortString())
}

// Compare returns true if other (the given BlockID) is less than this BlockID, by lexicographic comparison.
func (id BlockID) Compare(other BlockID) bool {
	return bytes.Compare(id.Bytes(), other.Bytes()) < 0
}

// AsHash32 returns a Hash32 whose first 20 bytes are the bytes of this BlockID, it is right-padded with zeros.
func (id BlockID) AsHash32() Hash32 {
	return Hash20(id).ToHash32()
}

var layersPerEpoch int32

// EffectiveGenesis marks when actual blocks would start being crated in the network, this will take account the first
// genesis epoch and the following epoch in which ATXs are published
var EffectiveGenesis int32

func getLayersPerEpoch() int32 {
	return atomic.LoadInt32(&layersPerEpoch)
}

// SetLayersPerEpoch sets global parameter of layers per epoch, all conversion from layer to epoch use this param
func SetLayersPerEpoch(layers int32) {
	atomic.StoreInt32(&layersPerEpoch, layers)
	atomic.StoreInt32(&EffectiveGenesis, layers*2-1)
}

// LayerID is a uint64 representing a layer number. It is zero-based.
type LayerID uint64

// GetEpoch returns the epoch number of this LayerID.
func (l LayerID) GetEpoch() EpochID {
	return EpochID(uint64(l) / uint64(getLayersPerEpoch()))
}

// GetEffectiveGenesis returns when actual blocks would be created
func GetEffectiveGenesis() LayerID {
	return LayerID(atomic.LoadInt32(&EffectiveGenesis))
}

// Add returns the LayerID that's layers (the param passed into this method) after l (this LayerID).
func (l LayerID) Add(layers uint16) LayerID {
	return LayerID(uint64(l) + uint64(layers))
}

// Uint64 returns the LayerID as a uint64.
func (l LayerID) Uint64() uint64 {
	return uint64(l)
}

// Field returns a log field. Implements the LoggableField interface.
func (l LayerID) Field() log.Field { return log.Uint64("layer_id", uint64(l)) }

// NodeID contains a miner's two public keys.
type NodeID struct {
	// Key is the miner's Edwards public key
	Key string

	// VRFPublicKey is the miner's public key used for VRF. The VRF scheme used is BLS.
	VRFPublicKey []byte
}

// String returns a string representation of the NodeID, for logging purposes.
// It implements the Stringer interface.
func (id NodeID) String() string {
	return id.Key + string(id.VRFPublicKey)
}

// ToBytes returns the byte representation of the Edwards public key.
func (id NodeID) ToBytes() []byte {
	return util.Hex2Bytes(id.String())
}

// ShortString returns a the first 5 characters of the ID, for logging purposes.
func (id NodeID) ShortString() string {
	name := id.Key
	return Shorten(name, 5)
}

// BytesToNodeID deserializes a byte slice into a NodeID
// TODO: length of the input will be made exact when the NodeID is compressed into
// one single key (https://github.com/spacemeshos/go-spacemesh/issues/2269)
func BytesToNodeID(b []byte) (*NodeID, error) {
	if len(b) < 32 {
		return nil, fmt.Errorf("invalid input length, input too short")
	}
	if len(b) > 64 {
		return nil, fmt.Errorf("invalid input length, input too long")
	}

	pubKey := b[0:32]
	vrfKey := b[32:]
	return &NodeID{
		Key:          util.Bytes2Hex(pubKey),
		VRFPublicKey: []byte(util.Bytes2Hex(vrfKey)),
	}, nil
}

// StringToNodeID deserializes a string into a NodeID
// TODO: length of the input will be made exact when the NodeID is compressed into
// one single key (https://github.com/spacemeshos/go-spacemesh/issues/2269)
func StringToNodeID(s string) (*NodeID, error) {
	strLen := len(s)
	if strLen < 64 {
		return nil, fmt.Errorf("invalid length, input too short")
	}
	if strLen > 128 {
		return nil, fmt.Errorf("invalid length, input too long")
	}
	// portion of the string corresponding to the Edwards public key
	pubKey := s[:64]
	vrfKey := s[64:]
	return &NodeID{
		Key:          pubKey,
		VRFPublicKey: []byte(vrfKey),
	}, nil
}

// Field returns a log field. Implements the LoggableField interface.
func (id NodeID) Field() log.Field { return log.String("node_id", id.Key) }

// BlockEligibilityProof includes the required values that, along with the miner's VRF public key, allow non-interactive
// block eligibility validation.
type BlockEligibilityProof struct {
	// J is the counter value used to generate this eligibility proof. Each value of J must only be used once.
	J uint32

	// Sig is the VRF signature from which the block's LayerID is derived.
	Sig []byte
}

// BlockHeader includes all of a block's fields, except the list of transaction IDs, activation transaction IDs and the
// signature.
// TODO: consider combining this with MiniBlock, since this type isn't used independently anywhere.
type BlockHeader struct {
	LayerIndex       LayerID
	ATXID            ATXID
	EligibilityProof BlockEligibilityProof
	Data             []byte
	Coin             bool

	BaseBlock BlockID

	AgainstDiff []BlockID
	ForDiff     []BlockID
	NeutralDiff []BlockID
}

// Layer returns the block's LayerID.
func (b BlockHeader) Layer() LayerID {
	return b.LayerIndex
}

// MiniBlock includes all of a block's fields, except for the signature. This structure is serialized and signed to
// produce the block signature.
type MiniBlock struct {
	BlockHeader
	TxIDs []TransactionID
	// ATXIDs    []ATXID
	ActiveSet *[]ATXID
	RefBlock  *BlockID
}

// Block includes all of a block's fields, including signature and a cache of the BlockID and MinerID.
type Block struct {
	MiniBlock
	// keep id and minerID private to prevent them from being serialized
	id        BlockID            // ⚠️ keep private
	minerID   *signing.PublicKey // ⚠️ keep private
	Signature []byte
}

// Bytes returns the serialization of the MiniBlock.
func (b *Block) Bytes() []byte {
	blkBytes, err := InterfaceToBytes(b.MiniBlock)
	if err != nil {
		log.Panic(fmt.Sprintf("could not extract block bytes, %v", err))
	}
	return blkBytes
}

// Fields returns an array of LoggableFields for logging
func (b *Block) Fields() []log.LoggableField {
	activeSet := 0
	if b.ActiveSet != nil {
		activeSet = len(*b.ActiveSet)
	}

	return []log.LoggableField{
		b.ID(),
		b.LayerIndex,
		b.LayerIndex.GetEpoch(),
		log.FieldNamed("miner_id", b.MinerID()),
		log.String("base_block", b.BaseBlock.String()),
		log.Int("supports", len(b.ForDiff)),
		log.Int("againsts", len(b.AgainstDiff)),
		log.Int("abstains", len(b.NeutralDiff)),
		b.ATXID,
		log.Uint32("eligibility_counter", b.EligibilityProof.J),
		log.FieldNamed("ref_block", b.RefBlock),
		log.Int("active_set_size", activeSet),
		log.Int("tx_count", len(b.TxIDs)),
	}
}

// ID returns the BlockID.
func (b *Block) ID() BlockID {
	return b.id
}

// Initialize calculates and sets the block's cached ID and MinerID. This should be called once all the other fields of
// the block are set.
func (b *Block) Initialize() {
	blockBytes, err := InterfaceToBytes(b.MiniBlock)
	if err != nil {
		panic("failed to marshal block: " + err.Error())
	}
	b.id = BlockID(CalcHash32(blockBytes).ToHash20())

	pubkey, err := ed25519.ExtractPublicKey(blockBytes, b.Signature)
	if err != nil {
		panic("failed to extract public key: " + err.Error())
	}
	b.minerID = signing.NewPublicKey(pubkey)
}

// Hash32 returns a Hash32 whose first 20 bytes are the bytes of this BlockID, it is right-padded with zeros.
// This implements the sync.item interface.
func (b Block) Hash32() Hash32 {
	return b.id.AsHash32()
}

// ShortString returns a the first 5 characters of the ID, for logging purposes.
func (b Block) ShortString() string {
	return b.id.AsHash32().ShortString()
}

// MinerID returns this block's miner's Edwards public key.
func (b *Block) MinerID() *signing.PublicKey {
	return b.minerID
}

// BlockIDs returns a slice of BlockIDs corresponding to the given blocks.
func BlockIDs(blocks []*Block) []BlockID {
	ids := make([]BlockID, 0, len(blocks))
	for _, block := range blocks {
		ids = append(ids, block.ID())
	}
	return ids
}

// BlockIdsField returns a list of loggable fields for a given list of BlockIDs
func BlockIdsField(ids []BlockID) log.Field {
	strs := []string{}
	for _, a := range ids {
		strs = append(strs, a.String())
	}
	return log.String("block_ids", strings.Join(strs, ", "))
}

// Layer contains a list of blocks and their corresponding LayerID.
type Layer struct {
	blocks []*Block
	index  LayerID
}

// Field returns a log field. Implements the LoggableField interface.
func (l *Layer) Field() log.Field {
	return log.String("layer",
		fmt.Sprintf("layerhash %s layernum %d numblocks %d", l.Hash().String(), l.index, len(l.blocks)))
}

// Index returns the layer's ID.
func (l *Layer) Index() LayerID {
	return l.index
}

// Blocks returns the list of blocks in this layer.
func (l *Layer) Blocks() []*Block {
	return l.blocks
}

// Hash returns the 32-byte sha256 sum of the block IDs in this layer, sorted in lexicographic order.
func (l Layer) Hash() Hash32 {
	return CalcBlocksHash32(SortBlockIDs(BlockIDs(l.blocks)), nil)
}

// AddBlock adds a block to this layer. Panics if the block's index doesn't match the layer.
func (l *Layer) AddBlock(block *Block) {
	if block.LayerIndex != l.index {
		log.Panic("add block with wrong layer number act %v exp %v", block.LayerIndex, l.index)
	}
	l.blocks = append(l.blocks, block)
}

// SetBlocks sets the list of blocks for the layer without validation.
func (l *Layer) SetBlocks(blocks []*Block) {
	l.blocks = blocks
}

// NewExistingLayer returns a new layer with the given list of blocks without validation.
func NewExistingLayer(idx LayerID, blocks []*Block) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}

// NewExistingBlock returns a block in the given layer with the given arbitrary data. The block is signed with a random
// keypair that isn't stored anywhere. This method should be phased out of use in production code (it's currently used
// in tests and the temporary genesis flow).
func NewExistingBlock(layerIndex LayerID, data []byte, txs []TransactionID) *Block {
	b := Block{
		MiniBlock: MiniBlock{
			BlockHeader: BlockHeader{
				LayerIndex: layerIndex,
				Data:       data},
			TxIDs: txs,
		}}
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())
	b.Initialize()
	return &b
}

// NewLayer returns a layer with no blocks.
func NewLayer(layerIndex LayerID) *Layer {
	return &Layer{
		index:  layerIndex,
		blocks: make([]*Block, 0, 10),
	}
}

// SortBlockIDs sorts a list of BlockIDs in lexicographic order, in-place.
func SortBlockIDs(ids []BlockID) []BlockID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// SortBlocks sorts a list of Blocks in lexicographic order of their IDs, in-place.
func SortBlocks(ids []*Block) []*Block {
	sort.Slice(ids, func(i, j int) bool { return ids[i].ID().Compare(ids[j].ID()) })
	return ids
}

// RandomBlockID generates random block id
func RandomBlockID() BlockID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return BlockID{}
	}
	return BlockID(CalcHash32(b).ToHash20())
}
