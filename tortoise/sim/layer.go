package sim

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NextOpt is for configuring layer generator.
type NextOpt func(*nextConf)

func nextConfDefaults() nextConf {
	return nextConf{}
}

type nextConf struct {
	Reorder uint32
}

// WithNextReorder configures when reordered layer should be returned.
func WithNextReorder(delay uint32) NextOpt {
	return func(c *nextConf) {
		c.Reorder = delay
	}
}

// Next generates next layer.
func (g *Generator) Next(opts ...NextOpt) types.LayerID {
	cfg := nextConfDefaults()
	for _, opt := range opts {
		opt(&cfg)
	}
	// TODO(dshulyak) we are not reoredering already reordered layer
	if lid, exist := g.reordered[g.nextLayer]; exist {
		delete(g.reordered, g.nextLayer)
		return lid
	}
	lid := g.genLayer()
	if cfg.Reorder != 0 {
		// Add(1) to account for generated layer at the end
		g.reordered[lid.Add(cfg.Reorder).Add(1)] = lid
		return g.genLayer()
	}
	return lid
}

func (g *Generator) genLayer() types.LayerID {
	half := int(g.conf.LayerSize) / 2
	numblocks := intInRange(g.rng, [2]int{half, half * 3})
	layer := types.NewLayer(g.nextLayer)
	for i := 0; i < numblocks; i++ {
		miner := g.rng.Intn(len(g.activations))
		atxid := g.activations[miner]
		signer := g.keys[miner]

		// TODO base block selection algorithm must be selected as an option
		// so that erroneous base block selection can be defined as a failure condition

		baseLayer := g.layers[len(g.layers)-1]
		support := g.layers[len(g.layers)-1].BlocksIDs()
		base := baseLayer.Blocks()[g.rng.Intn(len(baseLayer.Blocks()))]

		data := make([]byte, 20)
		g.rng.Read(data)
		block := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					LayerIndex: g.nextLayer,
					ATXID:      atxid,
					BaseBlock:  base.ID(),
					ForDiff:    support,
					Data:       data,
				},
			},
		}
		block.Signature = signer.Sign(block.Bytes())
		block.Initialize()
		if err := g.State.MeshDB.AddBlock(block); err != nil {
			g.logger.With().Panic("failed to add block", log.Err(err))
		}
		layer.AddBlock(block)
	}
	// TODO saving layer input should be skipped as one of the error conditions
	if err := g.State.MeshDB.SaveLayerInputVectorByID(context.Background(), layer.Index(), layer.BlocksIDs()); err != nil {
		g.logger.With().Panic("failed to save layer input vecotor", log.Err(err))
	}
	g.layers = append(g.layers, layer)
	g.nextLayer = g.nextLayer.Add(1)
	return layer.Index()
}
