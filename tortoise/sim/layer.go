package sim

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// DefaultNumBlocks is a number of blocks in a layer by default.
const DefaultNumBlocks = 5

// NextOpt is for configuring layer generator.
type NextOpt func(*nextConf)

func nextConfDefaults() nextConf {
	return nextConf{
		VoteGen:          PerfectVoting,
		Coinflip:         true,
		LayerSize:        -1,
		NumBlocks:        DefaultNumBlocks,
		BlockTickHeights: make([]uint64, DefaultNumBlocks),
	}
}

type nextConf struct {
	Reorder          uint32
	FailHare         bool
	EmptyHare        bool
	HareOutputIndex  int
	Coinflip         bool
	LayerSize        int
	NumBlocks        int
	BlockTickHeights []uint64
	VoteGen          VotesGenerator
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

// WithBlockTickHeights updates height of the blocks.
func WithBlockTickHeights(heights ...uint64) NextOpt {
	return func(c *nextConf) {
		c.BlockTickHeights = heights
	}
}

// WithNumBlocks sets number of the generated blocks.
func WithNumBlocks(num int) NextOpt {
	return func(c *nextConf) {
		c.NumBlocks = num
	}
}

// WithHareOutputIndex sets the index of the block that will be stored as a hare output.
func WithHareOutputIndex(i int) NextOpt {
	return func(c *nextConf) {
		c.HareOutputIndex = i
	}
}

// Next generates the next layer.
func (g *Generator) Next(opts ...NextOpt) types.LayerID {
	cfg := nextConfDefaults()
	for _, opt := range opts {
		opt(&cfg)
	}
	// TODO(dshulyak) we are not reordering already reordered layer
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

	miners := make([]uint32, len(g.activations))
	for i := 0; i < size; i++ {
		miner := i % len(g.activations)
		miners[miner]++
	}
	for miner, maxj := range miners {
		if maxj == 0 {
			continue
		}
		voting := cfg.VoteGen(g.rng, g.layers, miner)
		atxid := g.activations[miner]
		signer := g.keys[miner]
		proofs := []types.VotingEligibility{}
		for j := uint32(0); j < maxj; j++ {
			proofs = append(proofs, types.VotingEligibility{J: j})
		}
		beacon, err := g.states[0].Beacons.GetBeacon(g.nextLayer.GetEpoch())
		if err != nil {
			g.logger.With().Panic("failed to get a beacon", log.Err(err))
		}
		ballot := &types.Ballot{
			BallotMetadata: types.BallotMetadata{
				Layer: g.nextLayer,
			},
			InnerBallot: types.InnerBallot{
				AtxID: atxid,
				EpochData: &types.EpochData{
					ActiveSet: activeset,
					Beacon:    beacon,
				},
			},
			Votes:             voting,
			EligibilityProofs: proofs,
		}
		ballot.Signature = signer.Sign(signing.BALLOT, ballot.SignedBytes())
		ballot.SetSmesherID(signer.NodeID())
		if err = ballot.Initialize(); err != nil {
			g.logger.With().Panic("failed to init ballot", log.Err(err))
		}
		ballot.SetSmesherID(signer.NodeID())
		for _, state := range g.states {
			state.OnBallot(ballot)
		}
		layer.AddBallot(ballot)
	}
	if len(cfg.BlockTickHeights) < cfg.NumBlocks {
		g.logger.With().Panic("BlockTickHeights should be atleast to NumBlocks",
			log.Int("num blocks", cfg.NumBlocks),
		)
	}
	for i := 0; i < cfg.NumBlocks; i++ {
		block := &types.Block{}
		block.LayerIndex = g.nextLayer
		block.TxIDs = g.genTXIDs(3)
		block.TickHeight = cfg.BlockTickHeights[i]
		block.Initialize()
		for _, state := range g.states {
			state.OnBlock(block)
		}
		layer.AddBlock(block)
	}
	if !cfg.FailHare {
		hareOutput := types.EmptyBlockID
		if !cfg.EmptyHare && len(layer.BlocksIDs()) > 0 {
			hareOutput = layer.BlocksIDs()[cfg.HareOutputIndex]
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
