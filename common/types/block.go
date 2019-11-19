package types

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"sort"
)

type BlockID Hash32

func (l BlockID) String() string {
	return l.AsHash32().ShortString()
}

type LayerID uint64

func (l LayerID) GetEpoch(layersPerEpoch uint16) EpochId {
	return EpochId(uint64(l) / uint64(layersPerEpoch))
}

func (l LayerID) Add(layers uint16) LayerID {
	return LayerID(uint64(l) + uint64(layers))
}

func (l LayerID) Uint64() uint64 {
	return uint64(l)
}

//todo: choose which type is VRF
type Vrf string

type NodeId struct {
	Key          string
	VRFPublicKey []byte
}

func (id NodeId) String() string {
	return id.Key + string(id.VRFPublicKey)
}

func (id NodeId) ToBytes() []byte {
	return util.Hex2Bytes(id.String())
}

func (id NodeId) ShortString() string {
	name := id.Key
	if len(name) > 5 {
		name = name[:5]
	}
	return name
}

type BlockHeader struct {
	LayerIndex       LayerID
	ATXID            AtxId
	EligibilityProof BlockEligibilityProof
	Data             []byte
	Coin             bool
	Timestamp        int64
	BlockVotes       []BlockID
	ViewEdges        []BlockID
}

type Signed interface {
	Sig() []byte
	Bytes() []byte
	Data() interface{}
}

type Block struct {
	MiniBlock
	id        BlockID //important keep this private
	Signature []byte
}

type MiniBlock struct {
	BlockHeader
	TxIds  []TransactionId
	AtxIds []AtxId
}

func (b BlockID) AsHash32() Hash32 {
	return Hash32(b)
}

func (t *Block) Sig() []byte {
	return t.Signature
}

func (t *Block) Data() interface{} {
	return &t.MiniBlock
}

func (t *Block) Bytes() []byte {
	bytes, err := InterfaceToBytes(t.MiniBlock)
	if err != nil {
		panic(fmt.Sprintf("could not extract block bytes, %v", err))
	}
	return bytes
}

type BlockEligibilityProof struct {
	J   uint32
	Sig []byte
}

func (b *Block) Id() BlockID {
	return b.id
}

//should be used after all changed to a block are done
func (b *Block) CalcAndSetId() {
	blockBytes, err := InterfaceToBytes(b.MiniBlock)
	if err != nil {
		panic("failed to marshal transaction: " + err.Error())
	}
	b.id = BlockID(CalcHash32(blockBytes))
}

func (b Block) Hash32() Hash32 {
	return b.id.AsHash32()
}

func (b Block) ShortString() string {
	return b.id.AsHash32().ShortString()
}

func (b BlockHeader) Layer() LayerID {
	return b.LayerIndex
}

func (b *BlockHeader) AddVote(id BlockID) {
	//todo: do this in a sorted manner
	b.BlockVotes = append(b.BlockVotes, id)
}

func (b *BlockHeader) AddView(id BlockID) {
	//todo: do this in a sorted manner
	b.ViewEdges = append(b.ViewEdges, id)
}

func (b *Block) Compare(bl *Block) (bool, error) {
	bbytes, err := InterfaceToBytes(*b)
	if err != nil {
		return false, err
	}
	blbytes, err := InterfaceToBytes(*bl)
	if err != nil {
		return false, err
	}
	return bytes.Equal(bbytes, blbytes), nil
}

type Layer struct {
	blocks []*Block
	index  LayerID
}

func (l *Layer) Index() LayerID {
	return l.index
}

func (l *Layer) Blocks() []*Block {
	return l.blocks
}

func (l *Layer) Hash() Hash32 {
	bids := l.blocks
	keys := make([]BlockID, 0, len(bids))
	for _, tortoiseBlock := range bids {
		keys = append(keys, tortoiseBlock.Id())
	}
	hash, err := CalcBlocksHash32(keys)
	if err != nil {
		panic(fmt.Sprintf("failed to calculate layer's hash - layer Id %v", l.index))
	}
	return hash
}

func (l *Layer) AddBlock(block *Block) {
	block.LayerIndex = l.index
	l.blocks = append(l.blocks, block)
}

func (l *Layer) SetBlocks(blocks []*Block) {
	l.blocks = blocks
}

func NewExistingLayer(idx LayerID, blocks []*Block) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}

func NewExistingBlock(layerIndex LayerID, data []byte) *Block {
	b := Block{
		MiniBlock: MiniBlock{
			BlockHeader: BlockHeader{
				BlockVotes: make([]BlockID, 0, 10),
				ViewEdges:  make([]BlockID, 0, 10),
				LayerIndex: LayerID(layerIndex),
				Data:       data},
		}}

	b.CalcAndSetId()
	return &b
}

func NewLayer(layerIndex LayerID) *Layer {
	return &Layer{
		index:  layerIndex,
		blocks: make([]*Block, 0, 10),
	}
}

func SortBlockIds(ids []BlockID) []BlockID {
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i].ToBytes(), ids[j].ToBytes()) < 0 })
	return ids
}

func SortBlocks(ids []*Block) []*Block {
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i].Id().ToBytes(), ids[j].Id().ToBytes()) < 0 })
	return ids
}
