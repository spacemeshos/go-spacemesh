package fixture

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

func RLayers(layers ...result.Layer) []result.Layer {
	return layers
}

func RLayerNonFinal(lid types.LayerID, blocks ...result.Block) result.Layer {
	layer := result.Layer{
		Layer:  lid,
		Blocks: blocks,
	}
	return layer
}

func ROpinion(lid types.LayerID, opinion types.Hash32, blocks ...result.Block) result.Layer {
	layer := RLayer(lid, blocks...)
	layer.Opinion = opinion
	return layer
}

func RLayer(lid types.LayerID, blocks ...result.Block) result.Layer {
	layer := result.Layer{
		Layer:    lid,
		Verified: true,
		Blocks:   blocks,
	}
	return layer
}

type RBlockOpt func(*result.Block)

func Hare() RBlockOpt {
	return func(b *result.Block) {
		b.Hare = true
	}
}

func Valid() RBlockOpt {
	return func(b *result.Block) {
		b.Valid = true
	}
}

func Invalid() RBlockOpt {
	return func(b *result.Block) {
		b.Invalid = true
	}
}

func Data() RBlockOpt {
	return func(b *result.Block) {
		b.Data = true
	}
}

func Good() RBlockOpt {
	return func(b *result.Block) {
		b.Valid = true
		b.Hare = true
		b.Data = true
	}
}

func RBlock(id types.BlockID, opts ...RBlockOpt) result.Block {
	block := result.Block{}
	block.Header.ID = id
	for _, opt := range opts {
		opt(&block)
	}
	return block
}

func IDGen(sid string) (id types.BlockID) {
	copy(id[:], sid)
	return id
}
