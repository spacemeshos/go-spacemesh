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
		VoteGen:      PerfectVoting,
		Coinflip:     true,
		HareFraction: Frac(1, 1),
		LayerSize:    -1,
	}
}

type nextConf struct {
	Reorder      uint32
	FailHare     bool
	HareFraction Fraction
	Coinflip     bool
	LayerSize    int
	VoteGen      VotesGenerator
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

// WithoutInputVector will prevent from saving input vector.
func WithoutInputVector() NextOpt {
	return func(c *nextConf) {
		c.FailHare = true
	}
}

// WithEmptyInputVector will save empty input vector.
func WithEmptyInputVector() NextOpt {
	return func(c *nextConf) {
		c.HareFraction.Nominator = 0
	}
}

// WithPartialHare will set input vector only to a fraction of all known blocks.
// Input vector will be limited to nominator*size/denominator.
func WithPartialHare(nominator, denominator int) NextOpt {
	if denominator == 0 {
		panic("denominator can't be zero")
	}
	return func(c *nextConf) {
		c.HareFraction = Frac(nominator, denominator)
	}
}

// WithLayerSizeOverwrite overwrite expected layer size.
func WithLayerSizeOverwrite(size int) NextOpt {
	return func(c *nextConf) {
		c.LayerSize = size
	}
}

// WithVoteGenerator declares vote generator for a layer.
func WithVoteGenerator(gen VotesGenerator) NextOpt {
	return func(c *nextConf) {
		c.VoteGen = gen
	}
}

// WithCoin is to setup weak coin for voting. By default coin will support blocks.
func WithCoin(coin bool) NextOpt {
	return func(c *nextConf) {
		c.Coinflip = coin
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

func (g *Generator) genBeacon() {
	eid := g.nextLayer.Sub(1).GetEpoch()
	beacon, _ := g.beacons.GetBeacon(eid)
	if beacon != nil {
		return
	}
	beacon = make([]byte, 32)
	g.rng.Read(beacon)
	g.beacons.StoreBeacon(eid, beacon)
}

func (g *Generator) genLayer(cfg nextConf) types.LayerID {
	if g.nextLayer.Sub(1).GetEpoch() < g.nextLayer.GetEpoch() && g.nextLayer.Sub(1) != types.GetEffectiveGenesis() {
		g.renewAtxs()
		g.genBeacon()
	}

	layer := types.NewLayer(g.nextLayer)
	size := int(g.conf.LayerSize)
	if cfg.LayerSize >= 0 {
		size = cfg.LayerSize
	}
	for i := 0; i < size; i++ {
		miner := g.rng.Intn(len(g.activations))
		atxid := g.activations[miner]
		signer := g.keys[miner]

		voting := cfg.VoteGen(g.rng, g.layers, i)
		data := make([]byte, 20)
		g.rng.Read(data)
		beacon, err := g.beacons.GetBeacon(g.nextLayer.GetEpoch() - 1)
		if err != nil {
			g.logger.With().Panic("failed to get a beacon", log.Err(err))
		}
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
				TortoiseBeacon: beacon,
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
		bids := layer.BlocksIDs()
		frac := len(bids) * cfg.HareFraction.Nominator / cfg.HareFraction.Denominator
		if err := g.State.MeshDB.SaveLayerInputVectorByID(context.Background(), layer.Index(), bids[:frac]); err != nil {
			g.logger.With().Panic("failed to save layer input vecotor", log.Err(err))
		}
	}
	g.State.MeshDB.RecordCoinflip(context.Background(), layer.Index(), cfg.Coinflip)
	g.layers = append(g.layers, layer)
	g.nextLayer = g.nextLayer.Add(1)
	return layer.Index()
}
