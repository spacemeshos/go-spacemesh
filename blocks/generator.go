package blocks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	dbproposals "github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
)

var errInvalidATXID = errors.New("proposal ATXID invalid")

// Generator generates a block from proposals.
type Generator struct {
	logger log.Log
	cfg    Config
	once   sync.Once
	eg     errgroup.Group
	ctx    context.Context
	cancel func()

	hareCh   chan hare.LayerOutput
	cdb      *datastore.CachedDB
	msh      meshProvider
	conState conservativeState
	fetcher  system.ProposalFetcher
	cert     certifier
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
type GeneratorOpt func(*Generator)

// WithContext modifies default context.
func WithContext(ctx context.Context) GeneratorOpt {
	return func(g *Generator) {
		g.ctx = ctx
	}
}

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

// WithHareOutputChan sets the chan to listen to hare output.
func WithHareOutputChan(ch chan hare.LayerOutput) GeneratorOpt {
	return func(g *Generator) {
		g.hareCh = ch
	}
}

// NewGenerator creates new block generator.
func NewGenerator(
	cdb *datastore.CachedDB, cState conservativeState, m meshProvider, f system.ProposalFetcher, c certifier,
	opts ...GeneratorOpt,
) *Generator {
	g := &Generator{
		logger:   log.NewNop(),
		cfg:      defaultConfig(),
		ctx:      context.Background(),
		cdb:      cdb,
		msh:      m,
		conState: cState,
		fetcher:  f,
		cert:     c,
	}
	for _, opt := range opts {
		opt(g)
	}
	g.ctx, g.cancel = context.WithCancel(g.ctx)
	return g
}

// Start starts listening to hare output.
func (g *Generator) Start() {
	g.once.Do(func() {
		g.eg.Go(func() error {
			return g.run()
		})
	})
}

// Stop stops listening to hare output.
func (g *Generator) Stop() {
	g.cancel()
	err := g.eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		g.logger.With().Error("blockGen task failure", log.Err(err))
	}
}

func (g *Generator) run() error {
	for {
		select {
		case <-g.ctx.Done():
			return fmt.Errorf("context done: %w", g.ctx.Err())
		case out := <-g.hareCh:
			// TODO: change this to sequential block processing
			// https://github.com/spacemeshos/go-spacemesh/issues/3297
			g.eg.Go(func() error {
				_ = g.processHareOutput(out)
				return nil
			})
		}
	}
}

func (g *Generator) getProposals(pids []types.ProposalID) ([]*types.Proposal, error) {
	result := make([]*types.Proposal, 0, len(pids))
	var (
		p   *types.Proposal
		err error
	)
	for _, pid := range pids {
		if p, err = dbproposals.Get(g.cdb, pid); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (g *Generator) processHareOutput(out hare.LayerOutput) error {
	ctx := out.Ctx
	logger := g.logger.WithContext(ctx).WithFields(out.Layer)
	hareOutput := types.EmptyBlockID
	if len(out.Proposals) > 0 {
		// fetch proposals from peers if not locally available
		if err := g.fetcher.GetProposals(ctx, out.Proposals); err != nil {
			failFetchCnt.Inc()
			logger.With().Error("failed to fetch proposals", log.Err(err))
			return err
		}

		// now all proposals should be in local DB
		props, err := g.getProposals(out.Proposals)
		if err != nil {
			failErrCnt.Inc()
			logger.With().Warning("failed to get proposals locally", log.Err(err))
			return err
		}

		if block, err := g.generateBlock(ctx, out.Layer, props); err != nil {
			failGenCnt.Inc()
			return err
		} else if err = g.msh.AddBlockWithTXs(ctx, block); err != nil {
			failErrCnt.Inc()
			return err
		} else {
			blockOkCnt.Inc()
			hareOutput = block.ID()
		}
	} else {
		emptyOutputCnt.Inc()
	}

	if err := g.cert.RegisterForCert(ctx, out.Layer, hareOutput); err != nil {
		logger.With().Warning("failed to register hare output for certifying", log.Err(err))
	}

	if err := g.cert.CertifyIfEligible(ctx, logger.WithFields(hareOutput), out.Layer, hareOutput); err != nil {
		logger.With().Warning("failed to certify block", hareOutput, log.Err(err))
	}

	if err := g.msh.ProcessLayerPerHareOutput(ctx, out.Layer, hareOutput); err != nil {
		logger.With().Error("mesh failed to process layer", log.Err(err))
		return err
	}
	return nil
}

// generateBlock generates a block from the list of Proposal.
func (g *Generator) generateBlock(ctx context.Context, layerID types.LayerID, proposals []*types.Proposal) (*types.Block, error) {
	logger := g.logger.WithContext(ctx).WithFields(layerID, log.Int("num_proposals", len(proposals)))
	txIDs, err := g.conState.SelectBlockTXs(layerID, proposals)
	if err != nil {
		logger.With().Error("failed to select block txs", layerID, log.Err(err))
		return nil, fmt.Errorf("select block txs: %w", err)
	}
	tickHeight, rewards, err := g.extractCoinbasesAndHeight(logger, proposals)
	if err != nil {
		return nil, err
	}
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			TickHeight: tickHeight,
			Rewards:    rewards,
			TxIDs:      txIDs,
		},
	}
	b.Initialize()
	logger.With().Info("block generated", log.Inline(b))
	return b, nil
}

func (g *Generator) extractCoinbasesAndHeight(logger log.Log, props []*types.Proposal) (uint64, []types.AnyReward, error) {
	weights := make(map[types.Address]*big.Rat)
	coinbases := make([]types.Address, 0, len(props))
	max := uint64(0)
	for _, p := range props {
		if p.AtxID == *types.EmptyATXID {
			// this proposal would not have been validated
			logger.Error("proposal with invalid ATXID, skipping reward distribution", p.LayerIndex, p.ID())
			return 0, nil, errInvalidATXID
		}
		atx, err := g.cdb.GetAtxHeader(p.AtxID)
		if atx.BaseTickHeight > max {
			max = atx.BaseTickHeight
		}
		if err != nil {
			logger.With().Warning("proposal ATX not found", p.ID(), p.AtxID, log.Err(err))
			return 0, nil, fmt.Errorf("block gen get ATX: %w", err)
		}
		ballot := &p.Ballot
		weightPer, err := proposals.ComputeWeightPerEligibility(g.cdb, ballot, g.cfg.LayerSize, g.cfg.LayersPerEpoch)
		if err != nil {
			logger.With().Error("failed to calculate weight per eligibility", p.ID(), log.Err(err))
			return 0, nil, err
		}
		logger.With().Debug("weight per eligibility", p.ID(), log.Stringer("weight_per", weightPer))
		actual := weightPer.Mul(weightPer, new(big.Rat).SetUint64(uint64(len(ballot.EligibilityProofs))))
		if weight, ok := weights[atx.Coinbase]; !ok {
			weights[atx.Coinbase] = actual
			coinbases = append(coinbases, atx.Coinbase)
		} else {
			weights[atx.Coinbase].Add(weight, actual)
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
	return max, rewards, nil
}
