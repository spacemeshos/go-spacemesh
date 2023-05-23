package result

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Layer struct {
	Layer     types.LayerID
	Finalized bool
	Opinion   types.Hash32
	Blocks    []Block
}

// FirstValid returns first block that crossed positive tortoise threshold,
// or if layer didn't accumulate enough weight yet - use hare result.
func (l *Layer) FirstValid() types.BlockID {
	for _, block := range l.Blocks {
		if block.Valid && l.Finalized {
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

func (l Layer) String() string {
	return fmt.Sprintf("%d %+v", l.Layer, l.Blocks)
}

func (l *Layer) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer", l.Layer.Uint32())
	encoder.AddString("opinion", l.Opinion.ShortString())
	encoder.AddBool("finalized", l.Finalized)
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
	encoder.AddBool("invalid", b.Invalid)
	encoder.AddBool("hare", b.Hare)
	encoder.AddBool("data", b.Data)
	return nil
}
