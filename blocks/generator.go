package blocks

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"

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
func (g *Generator) GenerateBlock(layerID types.LayerID, proposals []*types.Proposal) (*types.UCBlock, error) {
	txIDs, txs, err := g.extractUniqueTXs(layerID, proposals)
	if err != nil {
		return nil, err
	}
	rewards, err := g.calculateSmesherRewards(layerID, proposals, txs)
	if err != nil {
		return nil, err
	}
	b := &types.UCBlock{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			Rewards:    rewards,
			TxIDs:      txIDs,
		},
	}
	b.Initialize()
	return b, nil
}

func (g *Generator) extractUniqueTXs(layerID types.LayerID, proposals []*types.Proposal) ([]types.TransactionID, []*types.Transaction, error) {
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
	txs, missing := g.meshDB.GetTransactions(txIDs)
	if len(missing) > 0 {
		g.logger.Error("could not find transactions %v from layer %v", missing, layerID)
		return nil, nil, errTXNotFound
	}
	return txIDs, txs, nil
}

func (g *Generator) calculateSmesherRewards(layerID types.LayerID, proposals []*types.Proposal, txs []*types.Transaction) ([]types.AnyReward, error) {
	rInfo := calculateRewardPerProposal(layerID, g.cfg, txs, len(proposals))
	g.logger.With().Info("reward calculated", log.Object("rewards", rInfo))
	rewards := make([]types.AnyReward, 0, len(proposals))
	for _, p := range proposals {
		if p.AtxID == *types.EmptyATXID {
			// this proposal would not have been validated
			g.logger.With().Error("proposal with invalid ATXID, skipping reward distribution", p.LayerIndex, p.ID())
			return nil, errInvalidATXID
		}
		atx, err := g.atxDB.GetAtxHeader(p.AtxID)
		if err != nil {
			g.logger.With().Warning("proposal ATX not found", p.ID(), p.AtxID, log.Err(err))
			return nil, fmt.Errorf("block gen get ATX: %w", err)
		}
		rewards = append(rewards, types.AnyReward{
			Address:     atx.Coinbase,
			SmesherID:   atx.NodeID,
			Amount:      rInfo.totalRewardPer,
			LayerReward: rInfo.layerRewardPer,
		})
	}
	sort.Slice(rewards[:], func(i, j int) bool {
		return bytes.Compare(rewards[i].Address.Bytes(), rewards[j].Address.Bytes()) < 0
	})
	return rewards, nil
}
