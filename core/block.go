package core

import "time"

type BLockId string

type Block struct {
	id         BLockId
	layerNum   uint64
	blockVotes map[BLockId]bool
	timestamp  time.Time
	coin       bool
	data       []byte
}

type Layer struct {
	blocks []Block
	layerNum uint64
	layerHash string
}