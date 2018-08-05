package core

import (
	"time"
	"github.com/google/uuid"
)

type BLockId uuid.UUID

var layerCounter uint64 = 0

type Block struct {
	id         BLockId
	layerNum   uint64
	blockVotes map[BLockId]bool
	timestamp  time.Time
	coin       bool
	data       []byte
}

func NewBlock(coin bool, data []byte, ts time.Time) Block{
	b := Block{
		id: BLockId(uuid.New()),
		blockVotes: make(map[BLockId]bool),
		timestamp: ts,
		data:data,
		coin:coin,
	}
	return b
}

type Layer struct {
	blocks []Block
	layerNum uint64
}

func (l *Layer) AddBlock(block Block){
	block.layerNum = l.layerNum
	l.blocks = append(l.blocks, block)
}

func NewLayer() Layer{
	l := Layer{
		blocks: make([]Block,0),
		layerNum: layerCounter,
	}
	layerCounter++
	return l
}