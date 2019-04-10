package consensus

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/types"
	"time"
)

var layerCounter types.LayerID = 0

type TortoiseBlock struct {
	Id         types.BlockID
	LayerIndex types.LayerID
	Data       []byte
	Coin       bool
	Timestamp  int64
	ProVotes   uint64
	ConVotes   uint64
	BlockVotes map[types.BlockID]bool
	ViewEdges  map[types.BlockID]struct{}
}

func (b TortoiseBlock) ID() types.BlockID {
	return b.Id
}

func (b TortoiseBlock) Layer() types.LayerID {
	return b.LayerIndex
}

func NewBlock(coin bool, data []byte, ts time.Time, LayerID types.LayerID) *TortoiseBlock {
	b := TortoiseBlock{
		Id:         types.BlockID(uuid.New().ID()),
		LayerIndex: LayerID,
		BlockVotes: make(map[types.BlockID]bool),
		ViewEdges:  make(map[types.BlockID]struct{}),
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
	index  types.LayerID
}

func (l *Layer) Index() types.LayerID {
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

func FromBlockToTortoiseBlock(blk *types.Block) *TortoiseBlock {
	bl := TortoiseBlock{
		Id:         types.BlockID(blk.Id),
		LayerIndex: types.LayerID(blk.LayerIndex),
		BlockVotes: make(map[types.BlockID]bool),
		ViewEdges:  make(map[types.BlockID]struct{}),
		Timestamp:  blk.Timestamp,
		Data:       blk.Data,
		Coin:       blk.Coin,
		ProVotes:   0,
		ConVotes:   0,
	}

	for _, id := range blk.BlockVotes {
		bl.BlockVotes[types.BlockID(id)] = true
	}
	return &bl
}

func FromLayerToTortoiseLayer(lyr *types.Layer) *Layer {
	l := Layer{
		index:  types.LayerID(lyr.Index()),
		blocks: make([]*TortoiseBlock, 0, len(lyr.Blocks())),
	}
	for _, block := range lyr.Blocks() {
		l.blocks = append(l.blocks, FromBlockToTortoiseBlock(block))
	}
	return &l
}

func NewExistingLayer(idx types.LayerID, blocks []*TortoiseBlock) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}
