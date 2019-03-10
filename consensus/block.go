package consensus

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"time"
)

var layerCounter mesh.LayerID = 0

type TortoiseBlock struct {
	Id         mesh.BlockID
	LayerIndex mesh.LayerID
	Data       []byte
	Coin       bool
	Timestamp  int64
	ProVotes   uint64
	ConVotes   uint64
	BlockVotes map[mesh.BlockID]bool
	ViewEdges  map[mesh.BlockID]struct{}
}

func (b TortoiseBlock) ID() mesh.BlockID {
	return b.Id
}

func (b TortoiseBlock) Layer() mesh.LayerID {
	return b.LayerIndex
}

func NewBlock(coin bool, data []byte, ts time.Time, LayerID mesh.LayerID) *TortoiseBlock {
	b := TortoiseBlock{
		Id:         mesh.BlockID(uuid.New().ID()),
		LayerIndex: LayerID,
		BlockVotes: make(map[mesh.BlockID]bool),
		ViewEdges:  make(map[mesh.BlockID]struct{}),
		Timestamp:  ts.UnixNano(),
		Data:       data,
		Coin:       coin,
		ProVotes:   0,
		ConVotes:   0,
	}
	return &b
}

type Layer struct {
	blocks []*TortoiseBlock
	index  mesh.LayerID
}

func (l *Layer) Index() mesh.LayerID {
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

func FromBlockToTortoiseBlock(block *mesh.Block) *TortoiseBlock {
	bl := TortoiseBlock{
		Id:         mesh.BlockID(block.Id),
		LayerIndex: mesh.LayerID(block.LayerIndex),
		BlockVotes: make(map[mesh.BlockID]bool),
		ViewEdges:  make(map[mesh.BlockID]struct{}),
		Timestamp:  block.Timestamp,
		Data:       block.Data,
		Coin:       block.Coin,
		ProVotes:   0,
		ConVotes:   0,
	}

	for _, id := range block.BlockVotes {
		bl.BlockVotes[mesh.BlockID(id)] = true
	}
	return &bl
}

func FromLayerToTortoiseLayer(lyr *mesh.Layer) *Layer {
	l := Layer{
		index:  mesh.LayerID(lyr.Index()),
		blocks: make([]*TortoiseBlock, 0, len(lyr.Blocks())),
	}
	for _, block := range lyr.Blocks() {
		l.blocks = append(l.blocks, FromBlockToTortoiseBlock(block))
	}
	return &l
}

func NewExistingLayer(idx mesh.LayerID, blocks []*TortoiseBlock) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
