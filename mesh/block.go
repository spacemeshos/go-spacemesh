package mesh

import (
	"github.com/google/uuid"
	"time"
)

type BlockID uint64
type LayerID uint64

var layerCounter LayerID = 0

type Block struct {
	id         BlockID
	layerId    LayerID
	blockVotes map[BlockID]bool
	timestamp  time.Time
	coin       bool
	data       []byte
	proVotes   uint64
	conVotes   uint64
}

func (b Block) Id() BlockID {
	return b.id
}

func (b Block) Layer() LayerID {
	return b.layerId
}

func NewExistingBlock(id BlockID, layerIndex LayerID, data []byte) *Block {
	b := Block{
		id:         BlockID(id),
		blockVotes: make(map[BlockID]bool),
		layerId:    LayerID(layerIndex),
		data:       data,
	}
	return &b
}

func NewBlock(coin bool, data []byte, ts time.Time, layerId LayerID) *Block {
	b := Block{
		id:         BlockID(uuid.New().ID()),
		layerId:    layerId,
		blockVotes: make(map[BlockID]bool),
		timestamp:  ts,
		data:       data,
		coin:       coin,
		proVotes:   0,
		conVotes:   0,
	}
	return &b
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

func (l *Layer) Hash() []byte {
	return []byte("some hash representing the layer")
}

func (l *Layer) AddBlock(block *Block) {
	block.layerId = l.index
	l.blocks = append(l.blocks, block)
}

func NewLayer() *Layer {
	l := Layer{
		blocks: make([]*Block, 0),
		index:  layerCounter,
	}
	layerCounter++
	return &l
}

func NewExistingLayer(idx LayerID, blocks []*Block) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
