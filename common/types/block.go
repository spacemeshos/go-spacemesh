package types

import (
	"bytes"
	"encoding/binary"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type BlockID uint64
type LayerID uint64

func (l LayerID) GetEpoch(layersPerEpoch uint16) EpochId {
	return EpochId(uint64(l) / uint64(layersPerEpoch))
}

func (l LayerID) Add(layers uint16) LayerID {
	return LayerID(uint64(l) + uint64(layers))
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
	Id               BlockID
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
	Signature []byte
}

type MiniBlock struct {
	BlockHeader
	TxIds  []TransactionId
	AtxIds []AtxId
}

func (t BlockID) AsHash32() Hash32 {
	b := make([]byte, 32)
	binary.LittleEndian.PutUint64(b, uint64(t))
	return BytesToHash(b)
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
		log.Panic("could not extract block bytes, %v", err)
	}
	return bytes
}

type BlockEligibilityProof struct {
	J   uint32
	Sig []byte
}

func newBlockHeader(id BlockID, layerID LayerID, coin bool, data []byte, ts int64, viewEdges []BlockID, blockVotes []BlockID) *BlockHeader {
	b := &BlockHeader{
		Id:         id,
		LayerIndex: layerID,
		BlockVotes: blockVotes,
		ViewEdges:  viewEdges,
		Timestamp:  ts,
		Data:       data,
		Coin:       coin,
	}
	return b
}

func (b BlockHeader) ID() BlockID {
	return b.Id
}

func (b BlockHeader) Hash32() Hash32 {
	return b.Id.AsHash32()
}

func (b BlockHeader) ShortString() string {
	return b.Id.AsHash32().ShortString()
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

func (b *Block) Compare(bl *Block) bool {
	bbytes, err := InterfaceToBytes(*b)
	if err != nil {
		log.Error("could not compare blocks %v", err)
		return false
	}
	blbytes, err := InterfaceToBytes(*bl)
	if err != nil {
		log.Error("could not compare blocks %v", err)
		return false
	}
	return bytes.Equal(bbytes, blbytes)
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
		keys = append(keys, tortoiseBlock.ID())
	}
	hash, err := CalcBlocksHash32(keys)
	if err != nil {
		log.Panic("failed to calculate layer's hash - layer Id %v", l.index)
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

func NewExistingBlock(id BlockID, layerIndex LayerID, data []byte) *Block {
	b := Block{
		MiniBlock: MiniBlock{
			BlockHeader: BlockHeader{
				Id:         BlockID(id),
				BlockVotes: make([]BlockID, 0, 10),
				ViewEdges:  make([]BlockID, 0, 10),
				LayerIndex: LayerID(layerIndex),
				Data:       data},
		}}
	return &b
}

func RandBlockId() BlockID {
	id := uuid.New()
	return BlockID(binary.BigEndian.Uint64(id[:8]))
}

func NewLayer(layerIndex LayerID) *Layer {
	return &Layer{
		index:  layerIndex,
		blocks: make([]*Block, 0, 10),
	}
}
