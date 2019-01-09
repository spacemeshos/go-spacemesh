package consensus

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/mesh"
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
	ViewEdges  map[BlockID]struct{}
}

func (b TortoiseBlock) ID() BlockID {
	return b.Id
}

func (b TortoiseBlock) Layer() LayerID {
	return b.LayerIndex
}



func NewBlock(coin bool, data []byte, ts time.Time, layerId LayerID) *TortoiseBlock {
	b := TortoiseBlock{
		Id:         BlockID(uuid.New().ID()),
		LayerIndex: layerId,
		BlockVotes: make(map[BlockID]bool),
		ViewEdges:  make(map[BlockID]struct{}),
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

func FromBlockToTortoiseBlock(block *mesh.Block) *TortoiseBlock{
	bl := TortoiseBlock{
		Id : BlockID(block.Id),
		LayerIndex: LayerID(block.LayerIndex),
		BlockVotes: make(map[BlockID]bool),
		ViewEdges:  make(map[BlockID]struct{}),
		Timestamp:  block.Timestamp,
		Data:       block.Data,
		Coin:       block.Coin,
		ProVotes:   0,
		ConVotes:   0,

	}

	for _, id := range block.BlockVotes{ bl.BlockVotes[BlockID(id)] = true }
	return &bl
}

func FromLayerToTortoiseLayer(layer *mesh.Layer) *Layer{
	l := Layer{
		index: LayerID(layer.Index()),
		blocks: make([]*TortoiseBlock,0,len(layer.Blocks())),
	}
	for _, block := range layer.Blocks(){ l.blocks = append(l.blocks, FromBlockToTortoiseBlock(block)) }
	return &l
}

func NewExistingLayer(idx LayerID, blocks []*TortoiseBlock) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
