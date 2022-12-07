package blocks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	dbproposals "github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
)

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
	conState conservativeState
	fetcher  system.ProposalFetcher
	cert     certifier

	hareCh      chan hare.LayerOutput
	hareOutputs map[types.LayerID]hare.LayerOutput
}

// Config is the config for Generator.
type Config struct {
	GenBlockInterval time.Duration
}

func defaultConfig() Config {
	return Config{
		GenBlockInterval: time.Second,
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
		logger:      log.NewNop(),
		cfg:         defaultConfig(),
		ctx:         context.Background(),
		cdb:         cdb,
		msh:         m,
		conState:    cState,
		fetcher:     f,
		cert:        c,
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
			g.logger.WithContext(out.Ctx).With().Info("received hare output",
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
		g.logger.With().Error("failed to get latest applied layer", log.Err(err))
		return
	}
	next := lastApplied.Add(1)
	for {
		if out, ok := g.hareOutputs[next]; !ok {
			break
		} else {
			g.logger.WithContext(out.Ctx).With().Info("ready to process hare output", next)
			_ = g.processHareOutput(out)
			delete(g.hareOutputs, next)
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
	if len(out.Proposals) == 0 {
		emptyOutputCnt.Inc()
	} else {
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

		block, executed, err = g.conState.GenerateBlock(ctx, out.Layer, props)
		if err != nil {
			failGenCnt.Inc()
			logger.With().Error("failed to generate block", log.Err(err))
			return err
		}

		logger.With().Info("block generated",
			log.Bool("executed", executed),
			log.Int("num_proposals", len(props)),
			log.Inline(block))

		err = g.msh.AddBlockWithTXs(ctx, block)
		if err != nil {
			failErrCnt.Inc()
			return err
		}

		blockOkCnt.Inc()
		hareOutput = block.ID()
	}

	if err := g.cert.RegisterForCert(ctx, out.Layer, hareOutput); err != nil {
		logger.With().Warning("failed to register hare output for certifying", log.Err(err))
	}

	if err := g.cert.CertifyIfEligible(ctx, logger.WithFields(hareOutput), out.Layer, hareOutput); err != nil {
		logger.With().Warning("failed to certify block", hareOutput, log.Err(err))
	}

	if err := g.msh.ProcessLayerPerHareOutput(ctx, out.Layer, hareOutput, executed); err != nil {
		logger.With().Error("mesh failed to process layer", log.Err(err))
		return err
	}
	return nil
}
