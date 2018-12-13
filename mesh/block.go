package mesh

import (
	"time"
	"github.com/google/uuid"
)

type BlockID uint32

var layerCounter uint64 = 0

type Block struct {
	id         BlockID
	layerNum   uint64
	blockVotes map[BlockID]bool
	timestamp  time.Time
	coin       bool
	data       []byte
	proVotes   uint64
	conVotes   uint64
}

func NewBlock(coin bool, data []byte, ts time.Time) *Block{
	b := Block{
		id:         BlockID(uuid.New().ID()),
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
	Blocks []Block
	layerNum uint64
}

func (l *Layer) AddBlock(block *Block){
	block.layerNum = l.layerNum
	l.Blocks = append(l.Blocks, *block)
}

func NewLayer() *Layer{
	l := Layer{
		Blocks: make([]Block,0),
		layerNum: layerCounter,
	}
	layerCounter++
	return &l
}

func (b Block) GetID() BlockID{
	return b.id
}