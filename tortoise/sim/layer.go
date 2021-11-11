package sim

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NextOpt is for configuring layer generator.
type NextOpt func(*nextConf)

func nextConfDefaults() nextConf {
	return nextConf{
		VoteGen: perfectVoting,
	}
}

type nextConf struct {
	Reorder  uint32
	FailHare bool
	VoteGen  VotesGenerator
}

// WithNextReorder configures when reordered layer should be returned.
// Examples:
// Next() Next(WithNextReorder(1)) Next()
// 1      3                        2
// Next() Next(WithNextReorder(2)) Next() Next()
// 1      3                        4      2
//
// So the Next layer with WithNextReorder will be delayed exactly by `delay` value.
func WithNextReorder(delay uint32) NextOpt {
	return func(c *nextConf) {
		c.Reorder = delay
	}
}

// WithHareFailure will prevent from saving input vector.
func WithHareFailure() NextOpt {
	return func(c *nextConf) {
		c.FailHare = true
	}
}

// WithVoteGenerator declares vote generator for a layer.
func WithVoteGenerator(gen VotesGenerator) NextOpt {
	return func(c *nextConf) {
		c.VoteGen = gen
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
	lid := g.genLayer(cfg)
	if cfg.Reorder != 0 {
		// Add(1) to account for generated layer at the end
		g.reordered[lid.Add(cfg.Reorder).Add(1)] = lid
		return g.genLayer(cfg)
	}
	return lid
}

func (g *Generator) genLayer(cfg nextConf) types.LayerID {
	layer := types.NewLayer(g.nextLayer)
	for i := 0; i < int(g.conf.LayerSize); i++ {
		miner := g.rng.Intn(len(g.activations))
		atxid := g.activations[miner]
		signer := g.keys[miner]

		// TODO base block selection algorithm must be selected as an option
		// so that erroneous base block selection can be defined as a failure condition

		voting := cfg.VoteGen(g.rng, g.layers)
		data := make([]byte, 20)
		g.rng.Read(data)
		block := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					LayerIndex:  g.nextLayer,
					ATXID:       atxid,
					BaseBlock:   voting.Base,
					ForDiff:     voting.Support,
					AgainstDiff: voting.Against,
					NeutralDiff: voting.Abstain,
					Data:        data,
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
	if !cfg.FailHare {
		if err := g.State.MeshDB.SaveLayerInputVectorByID(context.Background(), layer.Index(), layer.BlocksIDs()); err != nil {
			g.logger.With().Panic("failed to save layer input vecotor", log.Err(err))
		}
	}
	// TODO parametrize coinflip
	g.State.MeshDB.RecordCoinflip(context.Background(), layer.Index(), true)
	g.layers = append(g.layers, layer)
	g.nextLayer = g.nextLayer.Add(1)
	return layer.Index()
}
