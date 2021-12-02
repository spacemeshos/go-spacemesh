package blocks

import (
	"context"
	"errors"
	"fmt"
	"time"

	peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/blocks/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/system"
)

// NewBlockProtocol is the protocol indicator for gossip blocks.
const NewBlockProtocol = "newBlock"

var (
	errDupTx                 = errors.New("duplicate TransactionID in block")
	errDupAtx                = errors.New("duplicate ATXID in block")
	errNoActiveSet           = errors.New("block does not declare active set")
	errZeroActiveSet         = errors.New("block declares empty active set")
	errConflictingExceptions = errors.New("conflicting exceptions")
	errExceptionsOverlow     = errors.New("too many exceptions")
)

type mesh interface {
	GetBlock(types.BlockID) (*types.Block, error)
	AddBlockWithTxs(context.Context, *types.Block) error
}

type blockValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

// BlockHandler is the struct responsible for storing meta data needed to process blocks from gossip.
type BlockHandler struct {
	logger log.Log
	cfg    Config

	fetcher   system.Fetcher
	mesh      mesh
	validator blockValidator
}

// Config defines configuration for block handler.
type Config struct {
	MaxExceptions int
}

// defaultConfig for BlockHandler.
func defaultConfig() Config {
	return Config{
		MaxExceptions: 1000,
	}
}

// Opt for configuring BlockHandler.
type Opt func(b *BlockHandler)

// WithMaxExceptions defines max allowed exceptions in a block.
func WithMaxExceptions(max int) Opt {
	return func(b *BlockHandler) {
		b.cfg.MaxExceptions = max
	}
}

// WithLogger defines logger for BlockHandler.
func WithLogger(logger log.Log) Opt {
	return func(b *BlockHandler) {
		b.logger = logger
	}
}

// NewBlockHandler creates new BlockHandler.
func NewBlockHandler(fetcher system.Fetcher, m mesh, v blockValidator, opts ...Opt) *BlockHandler {
	b := &BlockHandler{
		logger:    log.NewNop(),
		cfg:       defaultConfig(),
		fetcher:   fetcher,
		mesh:      m,
		validator: v,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// HandleBlock handles blocks from gossip.
func (bh *BlockHandler) HandleBlock(ctx context.Context, _ peer.ID, msg []byte) pubsub.ValidationResult {
	if err := bh.HandleBlockData(ctx, msg); err != nil {
		bh.logger.WithContext(ctx).With().Warning("error handling block data", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

// HandleBlockData handles blocks from gossip and sync.
func (bh *BlockHandler) HandleBlockData(ctx context.Context, data []byte) error {
	logger := bh.logger.WithContext(ctx)
	logger.Debug("handling data for new block")
	start := time.Now()

	var blk types.Block
	if err := types.BytesToInterface(data, &blk); err != nil {
		logger.With().Error("received invalid block", log.Err(err))
		return fmt.Errorf("malformed block %w", err)
	}

	// set the block id when received
	blk.Initialize()

	logger = logger.WithFields(blk.ID(), blk.Layer())

	// check if known
	if _, err := bh.mesh.GetBlock(blk.ID()); err == nil {
		logger.Info("received known block")
		return nil
	}
	logger.With().Info("got new block", blk.Fields()...)

	if err := bh.blockSyntacticValidation(ctx, &blk); err != nil {
		return fmt.Errorf("failed to validate block %w", err)
	}

	saveMetrics(blk)

	if err := bh.mesh.AddBlockWithTxs(ctx, &blk); err != nil {
		return fmt.Errorf("adding block %s: %w", blk.ID(), err)
	}

	logger.With().Debug("time to process block", log.Duration("duration", time.Since(start)))
	return nil
}

func saveMetrics(blk types.Block) {
	type metric struct {
		hist  prometheus.Observer
		value float64
	}

	metricList := []metric{
		{
			hist:  metrics.LayerBlockSize.WithLabelValues(),
			value: float64(len(blk.Bytes())),
		},
		{
			hist:  metrics.NumTxsInBlock.WithLabelValues(),
			value: float64(len(blk.TxIDs)),
		},
		{
			hist:  metrics.BaseBlockExceptionLength.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeFor}),
			value: float64(len(blk.ForDiff)),
		},
		{
			hist:  metrics.BaseBlockExceptionLength.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeNeutral}),
			value: float64(len(blk.NeutralDiff)),
		},
		{
			hist:  metrics.BaseBlockExceptionLength.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeAgainst}),
			value: float64(len(blk.AgainstDiff)),
		},
	}

	for _, m := range metricList {
		m.hist.Observe(m.value)
	}
}

func blockDependencies(blk *types.Block) []types.BlockID {
	combined := []types.BlockID{blk.BaseBlock}
	combined = append(combined, blk.ForDiff...)
	combined = append(combined, blk.AgainstDiff...)
	combined = append(combined, blk.NeutralDiff...)
	return combined
}

func validateExceptions(block *types.Block, max int) error {
	exceptions := map[types.BlockID]struct{}{}
	for _, diff := range [][]types.BlockID{block.ForDiff, block.NeutralDiff, block.AgainstDiff} {
		for _, bid := range diff {
			_, exist := exceptions[bid]
			if exist {
				return fmt.Errorf("%w: block %s is referenced multiple times in exceptions of block %s",
					errConflictingExceptions, bid, block.ID())
			}
			exceptions[bid] = struct{}{}
		}
	}
	if len(exceptions) > max {
		return fmt.Errorf("%w: %d exceptions with max allowed %d in blocks %s",
			errExceptionsOverlow, len(exceptions), max, block.ID())
	}
	return nil
}

func (bh BlockHandler) blockSyntacticValidation(ctx context.Context, block *types.Block) error {
	// Add layer to context, for logging purposes, since otherwise the context will be lost here below
	if reqID, ok := log.ExtractRequestID(ctx); ok {
		ctx = log.WithRequestID(ctx, reqID, block.Layer())
	}

	bh.logger.WithContext(ctx).With().Debug("syntactically validating block", block.ID())

	if err := validateExceptions(block, bh.cfg.MaxExceptions); err != nil {
		return err
	}

	// if there is a reference block - first validate it
	if block.RefBlock != nil {
		err := bh.fetcher.FetchBlock(ctx, *block.RefBlock)
		if err != nil {
			return fmt.Errorf("failed to fetch ref block %v e: %v", *block.RefBlock, err)
		}
	}

	// try fetch referenced ATXs
	err := bh.fetchAllReferencedAtxs(ctx, block)
	if err != nil {
		return fmt.Errorf("fetch all referenced ATXs: %w", err)
	}

	// fast validation checks if there are no duplicate ATX in active set and no duplicate TXs as well
	if err := bh.fastValidation(block); err != nil {
		return fmt.Errorf("fast validation: %w", err)
	}

	// get the TXs
	if len(block.TxIDs) > 0 {
		err := bh.fetcher.GetTxs(ctx, block.TxIDs)
		if err != nil {
			return fmt.Errorf("failed to fetch txs %v e: %v", block.ID(), err)
		}
	}

	// get and validate blocks views using the fetch
	err = bh.fetcher.GetBlocks(ctx, blockDependencies(block))
	if err != nil {
		return fmt.Errorf("failed to fetch view %v e: %v", block.ID(), err)
	}

	bh.logger.WithContext(ctx).With().Debug("validation done: block is syntactically valid", block.ID())
	return nil
}

func (bh *BlockHandler) fetchAllReferencedAtxs(ctx context.Context, blk *types.Block) error {
	bh.logger.WithContext(ctx).With().Debug("block handler fetching all atxs referenced by block", blk.ID())

	// As block with empty or Golden ATXID is considered syntactically invalid, explicit check is not needed here.
	atxs := []types.ATXID{blk.ATXID}

	if blk.ActiveSet != nil {
		if len(*blk.ActiveSet) > 0 {
			atxs = append(atxs, *blk.ActiveSet...)
		} else {
			return errZeroActiveSet
		}
	} else {
		if blk.RefBlock == nil {
			return errNoActiveSet
		}
	}
	if len(atxs) > 0 {
		if err := bh.fetcher.GetAtxs(ctx, atxs); err != nil {
			return fmt.Errorf("get ATXs: %w", err)
		}

		return nil
	}

	return nil
}

func (bh *BlockHandler) fastValidation(block *types.Block) error {
	// block eligibility
	if eligible, err := bh.validator.BlockSignedAndEligible(block); err != nil || !eligible {
		return fmt.Errorf("block eligibility check failed: %w", err)
	}

	// validate unique tx atx
	if err := validateUniqueTxAtx(block); err != nil {
		return fmt.Errorf("validate unique tx ATX: %w", err)
	}
	return nil
}

func validateUniqueTxAtx(b *types.Block) error {
	// check for duplicate tx id
	mt := make(map[types.TransactionID]struct{}, len(b.TxIDs))
	for _, tx := range b.TxIDs {
		if _, exist := mt[tx]; exist {
			return errDupTx
		}
		mt[tx] = struct{}{}
	}

	// check for duplicate atx id
	if b.ActiveSet != nil {
		ma := make(map[types.ATXID]struct{}, len(*b.ActiveSet))
		for _, atx := range *b.ActiveSet {
			if _, exist := ma[atx]; exist {
				return errDupAtx
			}
			ma[atx] = struct{}{}
		}
	}

	return nil
}
