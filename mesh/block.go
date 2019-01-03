package mesh

import (
	"github.com/google/uuid"
	"time"
)

type BlockID uint32
type LayerID uint32

var layerCounter LayerID = 0



type TortoiseBlock struct {
	Id         BlockID
	LayerIndex LayerID
	Data       []byte
	Coin       bool
	Timestamp  time.Time
	ProVotes   uint64
	ConVotes   uint64
	BlockVotes map[BlockID]bool
}

func (b TortoiseBlock) ID() BlockID {
	return b.Id
}

func (b TortoiseBlock) Layer() LayerID {
	return b.LayerIndex
}

func NewExistingBlock(id BlockID, layerIndex LayerID, data []byte) *TortoiseBlock {
	b := TortoiseBlock{
		Id:         BlockID(id),
		BlockVotes: make(map[BlockID]bool),
		LayerIndex: LayerID(layerIndex),
		Data:       data,
	}
	return &b
}

func NewBlock(coin bool, data []byte, ts time.Time, layerId LayerID) *TortoiseBlock {
	b := TortoiseBlock{
		Id:         BlockID(uuid.New().ID()),
		LayerIndex: layerId,
		BlockVotes: make(map[BlockID]bool),
		Timestamp:  ts,
		Data:       data,
		Coin:       coin,
		ProVotes:   0,
		ConVotes:   0,
	}
	return &b
}

type Layer struct {
	blocks []*TortoiseBlock
	index  LayerID
}

func (l *Layer) Index() LayerID {
	return l.index
}

func (l *Layer) Blocks() []*TortoiseBlock {
	return l.blocks
}

func (l *Layer) Hash() []byte {
	return []byte("some hash representing the layer")
}

func (l *Layer) AddBlock(block *TortoiseBlock) {
	block.LayerIndex = l.index
	l.blocks = append(l.blocks, block)
}

func NewLayer() *Layer {
	l := Layer{
		blocks: make([]*TortoiseBlock, 0),
		index:  layerCounter,
	}
	layerCounter++
	return &l
}

func NewExistingLayer(idx LayerID, blocks []*TortoiseBlock) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
