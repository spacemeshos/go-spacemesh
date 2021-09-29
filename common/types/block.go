// Package types defines the types used by go-spacemesh consensus algorithms and structs
package types

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// LayerIDSize in bytes.
	LayerIDSize = 4
	// BlockIDSize in bytes.
	// FIXME(dshulyak) why do we cast to hash32 when returning bytes?
	BlockIDSize = Hash32Length
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

var (
	layersPerEpoch uint32
	// effectiveGenesis marks when actual blocks would start being created in the network. It takes into account
	// the first genesis epoch and the following epoch in which ATXs are published.
	effectiveGenesis uint32

	// EmptyLayerHash is the layer hash for an empty layer
	EmptyLayerHash = Hash32{}
)

// SetLayersPerEpoch sets global parameter of layers per epoch, all conversions from layer to epoch use this param
func SetLayersPerEpoch(layers uint32) {
	atomic.StoreUint32(&layersPerEpoch, layers)
	atomic.StoreUint32(&effectiveGenesis, layers*2-1)
}

func getLayersPerEpoch() uint32 {
	return atomic.LoadUint32(&layersPerEpoch)
}

// GetEffectiveGenesis returns when actual blocks would be created
func GetEffectiveGenesis() LayerID {
	return NewLayerID(atomic.LoadUint32(&effectiveGenesis))
}

// NewLayerID creates LayerID from uint32.
func NewLayerID(value uint32) LayerID {
	return LayerID{Value: value}
}

// LayerID is representing a layer number. Zero value is safe to use, and means 0.
// Internally it is a simple wrapper over uint32 and should be considered immutable
// the same way as any integer.
type LayerID struct {
	// NOTE(dshulyak) it is made public for compatibility with encoding library.
	// Don't modify it directly, as it will likely to be made private in the future.
	Value uint32
}

// GetEpoch returns the epoch number of this LayerID.
func (l LayerID) GetEpoch() EpochID {
	return EpochID(l.Value / getLayersPerEpoch())
}

// Add layers to the layer. Panics on wraparound.
func (l LayerID) Add(layers uint32) LayerID {
	nl := l.Value + layers
	if nl < l.Value {
		panic("layer_id wraparound")
	}
	l.Value = nl
	return l
}

// Sub layers from the layer. Panics on wraparound.
func (l LayerID) Sub(layers uint32) LayerID {
	if layers > l.Value {
		panic("layer_id wraparound")
	}
	l.Value -= layers
	return l
}

// OrdinalInEpoch returns layer ordinal in epoch.
func (l LayerID) OrdinalInEpoch() uint32 {
	return l.Value % getLayersPerEpoch()
}

// FirstInEpoch returns whether this LayerID is first in epoch.
func (l LayerID) FirstInEpoch() bool {
	return l.OrdinalInEpoch() == 0
}

// Mul layer by the layers. Panics on wraparound.
func (l LayerID) Mul(layers uint32) LayerID {
	if l.Value == 0 {
		return l
	}
	nl := l.Value * layers
	if nl/l.Value != layers {
		panic("layer_id wraparound")
	}
	l.Value = nl
	return l
}

// Uint32 returns the LayerID as a uint32.
func (l LayerID) Uint32() uint32 {
	return l.Value
}

// Before returns true if this layer is lower than the other.
func (l LayerID) Before(other LayerID) bool {
	return l.Value < other.Value
}

// After returns true if this layer is higher than the other.
func (l LayerID) After(other LayerID) bool {
	return l.Value > other.Value
}

// Difference returns the difference between current and other layer.
func (l LayerID) Difference(other LayerID) uint32 {
	if other.Value > l.Value {
		panic(fmt.Sprintf("other (%d) must be before or equal to this layer (%d)", other.Value, l.Value))
	}
	return l.Value - other.Value
}

// Field returns a log field. Implements the LoggableField interface.
func (l LayerID) Field() log.Field { return log.Uint32("layer_id", l.Value) }

// String returns string representation of the layer id numeric value.
func (l LayerID) String() string {
	return strconv.FormatUint(uint64(l.Value), 10)
}

// NodeID contains a miner's two public keys.
type NodeID struct {
	// Key is the miner's Edwards public key
	Key string

	// VRFPublicKey is the miner's public key used for VRF.
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

	BaseBlock BlockID

	AgainstDiff []BlockID // base block explicitly supports a block that we want to vote against
	ForDiff     []BlockID // any additional blocks we want to support that base block doesn't (incl. in newer layers)
	NeutralDiff []BlockID // blocks that the base block is explicitly for or against for which we are neutral.
	// NOTE on neutral votes: a base block is by default neutral on all blocks and layers that come after it, so
	// there's no need to explicitly add neutral votes for more recent layers.
	// TODO: optimize this data structure in two ways:
	//   - neutral votes are only ever for an entire layer, never for a subset of blocks.
	//   - collapse AgainstDiff and ForDiff into a single list.
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2369.
}

// Layer returns the block's LayerID.
func (b BlockHeader) Layer() LayerID {
	return b.LayerIndex
}

// MiniBlock includes all of a block's fields, except for the signature. This structure is serialized and signed to
// produce the block signature.
type MiniBlock struct {
	BlockHeader
	TxIDs          []TransactionID
	ActiveSet      *[]ATXID
	RefBlock       *BlockID
	TortoiseBeacon []byte
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

// DBBlock is a Block structure as it is stored in DB.
type DBBlock struct {
	MiniBlock
	// NOTE(dshulyak) this is a bit redundant to store ID here as well but less likely
	// to break if in future key for database will be changed
	ID        BlockID
	Signature []byte
	MinerID   []byte // derived from signature when block is received
}

// ToBlock create Block instance from data that is stored locally.
func (b *DBBlock) ToBlock() *Block {
	return &Block{
		id:        b.ID,
		MiniBlock: b.MiniBlock,
		Signature: b.Signature,
		minerID:   signing.NewPublicKey(b.MinerID),
	}
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
	var strs []string
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

// BlocksIDs returns the list of IDs of blocks in this layer.
func (l *Layer) BlocksIDs() []BlockID {
	blockIDs := make([]BlockID, len(l.blocks))
	for i := range l.blocks {
		blockIDs[i] = l.blocks[i].ID()
	}
	return blockIDs
}

// Hash returns the 32-byte sha256 sum of the block IDs of both contextually valid and invalid blocks in this layer,
// sorted in lexicographic order.
func (l Layer) Hash() Hash32 {
	if len(l.blocks) == 0 {
		return EmptyLayerHash
	}
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
				Data:       data,
			},
			TxIDs: txs,
		},
	}
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
