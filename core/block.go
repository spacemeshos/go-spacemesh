package core

import "time"

type BLockId string

type Block struct {
	id BLockId
	layerNum int64
	visibleBlocks map[BLockId]bool
	timestamp time.Time
	coin bool
	data []byte
}

type Layer struct {
	blocks []Block
	layerHash string
}