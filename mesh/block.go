package mesh

import (
	"time"
	"github.com/google/uuid"
)

type BLockID uint32

var layerCounter uint64 = 0

type Block struct {
	id         BLockID
	layerNum   uint64
	blockVotes map[BLockID]bool
	timestamp  time.Time
	coin       bool
	data       []byte
	proVotes   uint64
	conVotes   uint64
}

func NewBlock(coin bool, data []byte, ts time.Time) *Block{
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
	blocks []Block
	layerNum uint64
}

func (l *Layer) AddBlock(block *Block){
	block.layerNum = l.layerNum
	l.blocks = append(l.blocks, *block)
}

func NewLayer() *Layer{
	l := Layer{
		blocks: make([]Block,0),
		layerNum: layerCounter,
	}
	layerCounter++
	return &l
}