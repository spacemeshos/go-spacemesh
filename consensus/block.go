package consensus

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/block"
	"time"
)

var layerCounter block.LayerID = 0

type TortoiseBlock struct {
	Id         block.BlockID
	LayerIndex block.LayerID
	Data       []byte
	Coin       bool
	Timestamp  int64
	ProVotes   uint64
	ConVotes   uint64
	BlockVotes map[block.BlockID]bool
	ViewEdges  map[block.BlockID]struct{}
}

func (b TortoiseBlock) ID() block.BlockID {
	return b.Id
}

func (b TortoiseBlock) Layer() block.LayerID {
	return b.LayerIndex
}

func NewBlock(coin bool, data []byte, ts time.Time, LayerID block.LayerID) *TortoiseBlock {
	b := TortoiseBlock{
		Id:         block.BlockID(uuid.New().ID()),
		LayerIndex: LayerID,
		BlockVotes: make(map[block.BlockID]bool),
		ViewEdges:  make(map[block.BlockID]struct{}),
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
	index  block.LayerID
}

func (l *Layer) Index() block.LayerID {
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

func FromBlockToTortoiseBlock(blk *block.Block) *TortoiseBlock {
	bl := TortoiseBlock{
		Id:         block.BlockID(blk.Id),
		LayerIndex: block.LayerID(blk.LayerIndex),
		BlockVotes: make(map[block.BlockID]bool),
		ViewEdges:  make(map[block.BlockID]struct{}),
		Timestamp:  blk.Timestamp,
		Data:       blk.Data,
		Coin:       blk.Coin,
		ProVotes:   0,
		ConVotes:   0,
	}

	for _, id := range blk.BlockVotes {
		bl.BlockVotes[block.BlockID(id)] = true
	}
	return &bl
}

func FromLayerToTortoiseLayer(lyr *block.Layer) *Layer {
	l := Layer{
		index:  block.LayerID(lyr.Index()),
		blocks: make([]*TortoiseBlock, 0, len(lyr.Blocks())),
	}
	for _, block := range lyr.Blocks() {
		l.blocks = append(l.blocks, FromBlockToTortoiseBlock(block))
	}
	return &l
}

func NewExistingLayer(idx block.LayerID, blocks []*TortoiseBlock) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
