package sim

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const numBlocks = 5

// NextOpt is for configuring layer generator.
type NextOpt func(*nextConf)

func nextConfDefaults() nextConf {
	return nextConf{
		VoteGen:   PerfectVoting,
		Coinflip:  true,
		LayerSize: -1,
	}
}

type nextConf struct {
	Reorder   uint32
	FailHare  bool
	EmptyHare bool
	Coinflip  bool
	LayerSize int
	VoteGen   VotesGenerator
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

// WithoutHareOutput will prevent from saving hare output.
func WithoutHareOutput() NextOpt {
	return func(c *nextConf) {
		c.FailHare = true
	}
}

// WithEmptyHareOutput will save an empty vector for hare output.
func WithEmptyHareOutput() NextOpt {
	return func(c *nextConf) {
		c.EmptyHare = true
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

// Next generates the next layer.
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
	beacon := types.Beacon{}
	g.rng.Read(beacon[:])
	for _, state := range g.states {
		state.OnBeacon(eid, beacon)
	}
}

func (g *Generator) genTXIDs(n int) []types.TransactionID {
	txIDs := make([]types.TransactionID, 0, n)
	for i := 0; i < n; i++ {
		txid := types.TransactionID{}
		g.rng.Read(txid[:])
		txIDs = append(txIDs, txid)
	}
	return txIDs
}

func (g *Generator) genLayer(cfg nextConf) types.LayerID {
	if g.nextLayer.Sub(1).GetEpoch() < g.nextLayer.GetEpoch() {
		g.generateAtxs()
		g.genBeacon()
	}

	layer := types.NewLayer(g.nextLayer)
	size := int(g.conf.LayerSize)
	if cfg.LayerSize >= 0 {
		size = cfg.LayerSize
	}
	activeset := make([]types.ATXID, len(g.activations))
	copy(activeset, g.activations)

	miners := make(map[int]uint32, size)
	for i := 0; i < size; i++ {
		miner := g.rng.Intn(len(g.activations))
		atxid := g.activations[miner]
		signer := g.keys[miner]
		proof := types.VotingEligibilityProof{J: miners[miner]}
		miners[miner]++

		voting := cfg.VoteGen(g.rng, g.layers, i)
		beacon, err := g.states[0].Beacons.GetBeacon(g.nextLayer.GetEpoch())
		if err != nil {
			g.logger.With().Panic("failed to get a beacon", log.Err(err))
		}
		ballot := &types.Ballot{
			InnerBallot: types.InnerBallot{
				AtxID:            atxid,
				BaseBallot:       voting.Base,
				EligibilityProof: proof,
				AgainstDiff:      voting.Against,
				ForDiff:          voting.Support,
				NeutralDiff:      voting.Abstain,
				LayerIndex:       g.nextLayer,
				EpochData: &types.EpochData{
					ActiveSet: activeset,
					Beacon:    beacon,
				},
			},
		}
		ballot.Signature = signer.Sign(ballot.Bytes())
		if err = ballot.Initialize(); err != nil {
			g.logger.With().Panic("failed to init ballot", log.Err(err))
		}
		for _, state := range g.states {
			state.OnBallot(ballot)
		}
		layer.AddBallot(ballot)
	}
	for i := 0; i < numBlocks; i++ {
		block := types.GenLayerBlock(g.nextLayer, g.genTXIDs(3))
		for _, state := range g.states {
			state.OnBlock(block)
		}
		layer.AddBlock(block)
	}
	if !cfg.FailHare {
		hareOutput := types.EmptyBlockID
		if !cfg.EmptyHare {
			hareOutput = layer.BlocksIDs()[0]
		}
		for _, state := range g.states {
			state.OnHareOutput(layer.Index(), hareOutput)
		}
	}
	for _, state := range g.states {
		state.OnCoinflip(layer.Index(), cfg.Coinflip)
	}
	g.layers = append(g.layers, layer)
	g.nextLayer = g.nextLayer.Add(1)
	return layer.Index()
}
