package blocks

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	errInvalidATXID = errors.New("proposal ATXID invalid")
	errTXNotFound   = errors.New("proposal TX not found")
)

// Generator generates a block from proposals.
type Generator struct {
	logger log.Log
	cfg    RewardConfig
	atxDB  atxProvider
	meshDB meshProvider
}

func defaultConfig() RewardConfig {
	return RewardConfig{
		BaseReward: 50 * uint64(math.Pow10(12)),
	}
}

// GeneratorOpt for configuring BlockHandler.
type GeneratorOpt func(h *Generator)

// WithConfig defines cfg for Generator.
func WithConfig(cfg RewardConfig) GeneratorOpt {
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
func NewGenerator(atxDB atxProvider, meshDB meshProvider, opts ...GeneratorOpt) *Generator {
	g := &Generator{
		logger: log.NewNop(),
		cfg:    defaultConfig(),
		atxDB:  atxDB,
		meshDB: meshDB,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// GenerateBlock generates a block from the list of Proposal.
func (g *Generator) GenerateBlock(ctx context.Context, layerID types.LayerID, proposals []*types.Proposal) (*types.Block, error) {
	logger := g.logger.WithContext(ctx).WithFields(layerID, log.Int("num_proposals", len(proposals)))
	types.SortProposals(proposals)
	txIDs, txs, err := g.extractOrderedUniqueTXs(layerID, proposals)
	if err != nil {
		return nil, err
	}
	rewards, err := g.calculateSmesherRewards(logger, layerID, proposals, txs)
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

func (g *Generator) extractOrderedUniqueTXs(layerID types.LayerID, proposals []*types.Proposal) ([]types.TransactionID, []*types.Transaction, error) {
	seen := make(map[types.TransactionID]struct{})
	var txIDs []types.TransactionID
	for _, p := range proposals {
		for _, id := range p.TxIDs {
			if _, exist := seen[id]; exist {
				continue
			}
			txIDs = append(txIDs, id)
			seen[id] = struct{}{}
		}
	}
	types.SortTransactionIDs(txIDs)
	txs, missing := g.meshDB.GetTransactions(txIDs)
	if len(missing) > 0 {
		g.logger.Error("could not find transactions %v from layer %v", missing, layerID)
		return nil, nil, errTXNotFound
	}
	return txIDs, txs, nil
}

func (g *Generator) calculateSmesherRewards(logger log.Log, layerID types.LayerID, proposals []*types.Proposal, txs []*types.Transaction) ([]types.AnyReward, error) {
	eligibilities := 0
	for _, proposal := range proposals {
		eligibilities += len(proposal.EligibilityProofs)
	}
	rInfo := calculateRewardPerEligibility(layerID, g.cfg, txs, 50)
	logger.With().Info("reward calculated", log.Inline(rInfo))
	rewards := make([]types.AnyReward, 0, len(proposals))
	for _, p := range proposals {
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
		rewards = append(rewards, types.AnyReward{
			Address:     atx.Coinbase,
			SmesherID:   atx.NodeID,
			Amount:      rInfo.totalRewardPer * uint64(len(p.EligibilityProofs)),
			LayerReward: rInfo.layerRewardPer * uint64(len(p.EligibilityProofs)),
		})
	}
	return rewards, nil
}
