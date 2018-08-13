package core

import (
	"time"
	"github.com/google/uuid"
)

type BLockId string//uuid.UUID

var layerCounter uint64 = 0

type Block struct {
	id         BLockId
	layerNum   uint64
	blockVotes map[BLockId]bool
	timestamp  time.Time
	coin       bool
	data       []byte
	proVotes	uint64
	conVotes	uint64
}

func NewBlock(coin bool, data []byte, ts time.Time) *Block{
	b := Block{
		id: BLockId(uuid.New().String()),
		blockVotes: make(map[BLockId]bool),
		timestamp: ts,
		data:data,
		coin:coin,
		proVotes:0,
		conVotes:0,
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