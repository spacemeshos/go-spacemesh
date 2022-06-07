package blocks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
)

var errInvalidATXID = errors.New("proposal ATXID invalid")

// Generator generates a block from proposals.
type Generator struct {
	logger   log.Log
	cfg      Config
	atxDB    atxProvider
	meshDB   meshProvider
	conState conservativeState
}

// Config is the config for Generator.
type Config struct {
	LayerSize      uint32
	LayersPerEpoch uint32
}

func defaultConfig() Config {
	return Config{
		LayerSize:      50,
		LayersPerEpoch: 3,
	}
}

// GeneratorOpt for configuring Generator.
type GeneratorOpt func(h *Generator)

// WithConfig defines cfg for Generator.
func WithConfig(cfg Config) GeneratorOpt {
	return func(g *Generator) {
		g.cfg = cfg
	}
}

// WithGeneratorLogger defines logger for Generator.
func WithGeneratorLogger(logger log.Log) GeneratorOpt {
	return func(g *Generator) {
		g.logger = logger
	}
}

// NewGenerator creates new block generator.
func NewGenerator(atxDB atxProvider, meshDB meshProvider, cState conservativeState, opts ...GeneratorOpt) *Generator {
	g := &Generator{
		logger:   log.NewNop(),
		cfg:      defaultConfig(),
		atxDB:    atxDB,
		meshDB:   meshDB,
		conState: cState,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// GenerateBlock generates a block from the list of Proposal.
func (g *Generator) GenerateBlock(ctx context.Context, layerID types.LayerID, proposals []*types.Proposal) (*types.Block, error) {
	logger := g.logger.WithContext(ctx).WithFields(layerID, log.Int("num_proposals", len(proposals)))
	txIDs, err := g.conState.SelectBlockTXs(layerID, proposals)
	if err != nil {
		logger.With().Error("failed to select block txs", layerID, log.Err(err))
		return nil, fmt.Errorf("select block txs: %w", err)
	}
	rewards, err := g.calculateCoinbaseWeight(logger, proposals)
	if err != nil {
		return nil, err
	}
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			Rewards:    rewards,
			TxIDs:      txIDs,
		},
	}
	b.Initialize()
	logger.With().Info("block generated", log.Inline(b))
	return b, nil
}

func (g *Generator) calculateCoinbaseWeight(logger log.Log, props []*types.Proposal) ([]types.AnyReward, error) {
	weights := make(map[types.Address]util.Weight)
	coinbases := make([]types.Address, 0, len(props))
	for _, p := range props {
		if p.AtxID == *types.EmptyATXID {
			// this proposal would not have been validated
			logger.Error("proposal with invalid ATXID, skipping reward distribution", p.LayerIndex, p.ID())
			return nil, errInvalidATXID
		}
		atx, err := g.atxDB.GetAtxHeader(p.AtxID)
		if err != nil {
			logger.With().Warning("proposal ATX not found", p.ID(), p.AtxID, log.Err(err))
			return nil, fmt.Errorf("block gen get ATX: %w", err)
		}
		ballot := &p.Ballot
		weightPer, err := proposals.ComputeWeightPerEligibility(g.atxDB, g.meshDB, ballot, g.cfg.LayerSize, g.cfg.LayersPerEpoch)
		if err != nil {
			logger.With().Error("failed to calculate weight per eligibility", p.ID(), log.Err(err))
			return nil, err
		}
		logger.With().Debug("weight per eligibility", p.ID(), log.Stringer("weight_per", weightPer))
		actual := weightPer.Mul(util.WeightFromUint64(uint64(len(ballot.EligibilityProofs))))
		if _, ok := weights[atx.Coinbase]; !ok {
			weights[atx.Coinbase] = actual
			coinbases = append(coinbases, atx.Coinbase)
		} else {
			weights[atx.Coinbase].Add(actual)
		}
		events.ReportProposal(events.ProposalIncluded, p)
	}
	// make sure we output coinbase in a stable order.
	sort.Slice(coinbases, func(i, j int) bool {
		return bytes.Compare(coinbases[i].Bytes(), coinbases[j].Bytes()) < 0
	})
	rewards := make([]types.AnyReward, 0, len(weights))
	for _, coinbase := range coinbases {
		weight, ok := weights[coinbase]
		if !ok {
			g.logger.With().Fatal("coinbase missing", coinbase)
		}
		rewards = append(rewards, types.AnyReward{
			Coinbase: coinbase,
			Weight: types.RatNum{
				Num:   weight.Num().Uint64(),
				Denom: weight.Denom().Uint64(),
			},
		})
		logger.With().Debug("adding coinbase weight",
			log.Stringer("coinbase", coinbase),
			log.Stringer("weight", weight))
	}
	return rewards, nil
}
