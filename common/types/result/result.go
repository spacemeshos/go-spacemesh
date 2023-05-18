package result

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Layer struct {
	Layer   types.LayerID
	Opinion types.Hash32
	Blocks  []Block
}

// FirstValid returns first block that is considered valid by tortoise
// or first block considered valid by hare, while tortoise didn't decide yet.
func (l *Layer) FirstValid() types.BlockID {
	for _, block := range l.Blocks {
		if block.Valid {
			return block.Header.ID
		}
	}
	for _, block := range l.Blocks {
		if block.Hare && !block.Invalid {
			return block.Header.ID
		}
	}
	return types.EmptyBlockID
}

func (l *Layer) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer", l.Layer.Uint32())
	encoder.AddString("opinion", l.Opinion.ShortString())
	encoder.AddArray("blocks", log.ArrayMarshalerFunc(func(aencoder log.ArrayEncoder) error {
		for i := range l.Blocks {
			aencoder.AppendObject(&l.Blocks[i])
		}
		return nil
	}))
	return nil
}

type Block struct {
	Header         types.Vote
	Valid, Invalid bool
	Hare           bool
	Data           bool
}

func (b *Block) MarshalLogObject(encoder log.ObjectEncoder) error {
	b.Header.MarshalLogObject(encoder)
	encoder.AddBool("valid", b.Valid)
	encoder.AddBool("invalid", b.Valid)
	encoder.AddBool("hare", b.Hare)
	encoder.AddBool("data", b.Data)
	return nil
}
