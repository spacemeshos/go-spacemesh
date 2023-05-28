package blocks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	heligibility "github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
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

	cdb      *datastore.CachedDB
	msh      meshProvider
	executor executor
	fetcher  system.ProposalFetcher
	cert     certifier
	patrol   layerPatrol

	hareCh      chan hare.LayerOutput
	hareOutputs map[types.LayerID]hare.LayerOutput
}

// Config is the config for Generator.
type Config struct {
	LayerSize          uint32
	LayersPerEpoch     uint32
	GenBlockInterval   time.Duration
	BlockGasLimit      uint64
	OptFilterThreshold int
}

func defaultConfig() Config {
	return Config{
		LayerSize:          50,
		LayersPerEpoch:     3,
		GenBlockInterval:   time.Second,
		BlockGasLimit:      math.MaxUint64,
		OptFilterThreshold: 90,
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
	cdb *datastore.CachedDB,
	exec executor,
	m meshProvider,
	f system.ProposalFetcher,
	c certifier,
	p layerPatrol,
	opts ...GeneratorOpt,
) *Generator {
	g := &Generator{
		logger:      log.NewNop(),
		cfg:         defaultConfig(),
		ctx:         context.Background(),
		cdb:         cdb,
		msh:         m,
		executor:    exec,
		fetcher:     f,
		cert:        c,
		patrol:      p,
		hareOutputs: map[types.LayerID]hare.LayerOutput{},
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
			g.logger.WithContext(out.Ctx).With().Debug("received hare output",
				out.Layer,
				log.Int("num_proposals", len(out.Proposals)))
			g.hareOutputs[out.Layer] = out
			g.tryGenBlock()
		case <-time.After(g.cfg.GenBlockInterval):
			if len(g.hareOutputs) > 0 {
				g.tryGenBlock()
			}
		}
	}
}

func (g *Generator) tryGenBlock() {
	lastApplied, err := layers.GetLastApplied(g.cdb)
	if err != nil {
		g.logger.Error("failed to get latest applied layer", log.Err(err))
		return
	}
	next := lastApplied.Add(1)
	for {
		if out, ok := g.hareOutputs[next]; !ok {
			break
		} else {
			g.logger.WithContext(out.Ctx).With().Debug("ready to process hare output", next)
			_ = g.processHareOutput(out)
			delete(g.hareOutputs, next)
			g.patrol.CompleteHare(next)
			next = next.Add(1)
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
	var (
		block    *types.Block
		executed bool
	)
	if len(out.Proposals) > 0 {
		// fetch proposals from peers if not locally available
		if err := g.fetcher.GetProposals(ctx, out.Proposals); err != nil {
			failFetchCnt.Inc()
			logger.With().Warning("failed to fetch proposals", log.Err(err))
			return err
		}

		// now all proposals should be in local DB
		props, err := g.getProposals(out.Proposals)
		if err != nil {
			failErrCnt.Inc()
			logger.With().Warning("failed to get proposals locally", log.Err(err))
			return err
		}

		block, executed, err = g.generateBlock(ctx, logger, out.Layer, props)
		if err != nil {
			logger.With().Error("failed to generate block", log.Err(err))
			failGenCnt.Inc()
			return err
		} else if err = g.msh.AddBlockWithTXs(ctx, block); err != nil {
			logger.With().Error("failed to add block", log.Err(err))
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
		if errors.Is(err, heligibility.ErrNotActive) {
			logger.With().Debug("smesher is not active", log.Err(err))
		} else {
			logger.With().Warning("failed to certify block", hareOutput, log.Err(err))
		}
	}

	if err := g.msh.ProcessLayerPerHareOutput(ctx, out.Layer, hareOutput, executed); err != nil {
		logger.With().Error("mesh failed to process layer", log.Err(err))
		return err
	}
	return nil
}

// generateBlock combines the transactions in the proposals and put them in a stable order.
// if optimistic filtering is ON, it also executes the transactions in situ.
// else, prune the transactions up to block gas limit allows.
func (g *Generator) generateBlock(ctx context.Context, logger log.Log, lid types.LayerID, proposals []*types.Proposal) (*types.Block, bool, error) {
	md, err := getProposalMetadata(logger, g.cdb, g.cfg, lid, proposals)
	if err != nil {
		return nil, false, fmt.Errorf("collect proposal metadata: %w", err)
	}
	if md.optFilter {
		return g.genBlockOptimistic(ctx, logger, md)
	}

	var tids []types.TransactionID
	if len(md.mtxs) > 0 {
		blockSeed := types.CalcProposalsHash32(types.ToProposalIDs(proposals), nil).Bytes()
		tids, err = getBlockTXs(logger, md, blockSeed, g.cfg.BlockGasLimit)
		if err != nil {
			return nil, false, err
		}
	}
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: lid,
			TickHeight: md.tickHeight,
			Rewards:    md.rewards,
			TxIDs:      tids,
		},
	}
	b.Initialize()
	logger.With().Debug("block generated", log.Inline(b))
	return b, false, nil
}

func (g *Generator) genBlockOptimistic(ctx context.Context, logger log.Log, md *proposalMetadata) (*types.Block, bool, error) {
	var (
		tids []types.TransactionID
		err  error
	)
	if len(md.mtxs) > 0 {
		blockSeed := types.CalcProposalsHash32(types.ToProposalIDs(md.proposals), nil).Bytes()
		tids, err = getBlockTXs(logger, md, blockSeed, 0)
		if err != nil {
			return nil, false, err
		}
	}
	logger.With().Debug("executing txs in situ", log.Int("num_txs", len(tids)))
	block, err := g.executor.ExecuteOptimistic(ctx, md.lid, md.tickHeight, md.rewards, tids)
	if err != nil {
		return nil, false, fmt.Errorf("execute in situ: %w", err)
	}
	logger.With().Debug("block generated and executed", log.Inline(block))
	return block, true, nil
}
