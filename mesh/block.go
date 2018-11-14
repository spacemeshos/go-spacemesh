package mesh

import (
	"github.com/google/uuid"
	"time"
)

type BLockID uint32

var layerCounter uint32 = 0

type Block struct {
	id         BLockID
	layerIndex uint32
	blockVotes map[BLockID]bool
	timestamp  time.Time
	coin       bool
	data       []byte
	proVotes   uint64
	conVotes   uint64
}

func (b Block) Id() uint32 {
	return uint32(b.id)
}

func (b Block) Layer() uint32 {
	return b.layerIndex
}

func NewExistingBlock(id uint32, layerIndex uint32, data []byte) *Block {
	b := Block{
		id:         BLockID(id),
		blockVotes: make(map[BLockID]bool),
		layerIndex: layerIndex,
		data:       data,
	}
	return &b
}

func NewBlock(coin bool, data []byte, ts time.Time) *Block {
	b := Block{
		id:         BLockID(uuid.New().ID()),
		blockVotes: make(map[BLockID]bool),
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
	index  uint32
}

func (l *Layer) Index() int {
	return int(l.index)
}

func (l *Layer) Blocks() []*Block {
	return l.blocks
}

func (l *Layer) Hash() string {
	return "some hash representing the layer"
}

func (l *Layer) AddBlock(block Block) {
	block.layerIndex = l.index
	l.blocks = append(l.blocks, &block)
}

func NewLayer() *Layer {
	l := Layer{
		blocks: make([]*Block, 0),
		index:  layerCounter,
	}
	layerCounter++
	return &l
}

func NewExistingLayer(idx uint32, blocks []*Block) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
