package result

import "github.com/spacemeshos/go-spacemesh/common/types"

type Layer struct {
	Layer   types.LayerID
	Opinion types.Hash32
	Blocks  []Block
}

func (l *Layer) FirstValid() types.BlockID {
	for _, block := range l.Blocks {
		if block.Valid {
			return block.Header.ID
		}
	}
	return types.EmptyBlockID
}

func (l *Layer) FirstHare() types.BlockID {
	for _, block := range l.Blocks {
		if block.Hare {
			return block.Header.ID
		}
	}
	return types.EmptyBlockID
}

type Block struct {
	Header types.Vote
	Valid  bool
	Hare   bool
	Data   bool
}
