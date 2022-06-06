package blocks

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/p2p"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errMalformedData = errors.New("malformed data")
	errDuplicateTX   = errors.New("duplicate TxID in proposal")
)

// Handler processes Block fetched from peers during sync.
type Handler struct {
	logger log.Log

	fetcher system.Fetcher
	mesh    meshProvider
}

// Opt for configuring BlockHandler.
type Opt func(*Handler)

// WithLogger defines logger for Handler.
func WithLogger(logger log.Log) Opt {
	return func(h *Handler) {
		h.logger = logger
	}
}

// NewHandler creates new Handler.
func NewHandler(f system.Fetcher, m meshProvider, opts ...Opt) *Handler {
	h := &Handler{
		logger:  log.NewNop(),
		fetcher: f,
		mesh:    m,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleBlockData handles Block data from sync.
func (h *Handler) HandleBlockData(ctx context.Context, data []byte, _ p2p.Peer) error {
	logger := h.logger.WithContext(ctx)
	logger.Info("processing block")

	var b types.Block
	if err := codec.Decode(data, &b); err != nil {
		logger.With().Error("malformed block", log.Err(err))
		return errMalformedData
	}

	// set the block ID when received
	b.Initialize()

	if h.mesh.HasBlock(b.ID()) {
		logger.Info("known block")
		return nil
	}
	logger.With().Info("new block", log.Inline(&b))

	if err := h.checkTransactions(ctx, &b); err != nil {
		logger.With().Warning("failed to fetch block TXs", log.Err(err))
		return err
	}

	if err := h.mesh.AddBlockWithTXs(ctx, &b); err != nil {
		logger.With().Error("failed to save block", log.Err(err))
		return fmt.Errorf("save block: %w", err)
	}

	return nil
}

func (h *Handler) checkTransactions(ctx context.Context, b *types.Block) error {
	if len(b.TxIDs) == 0 {
		return nil
	}

	set := make(map[types.TransactionID]struct{}, len(b.TxIDs))
	for _, tx := range b.TxIDs {
		if _, exist := set[tx]; exist {
			return errDuplicateTX
		}
		set[tx] = struct{}{}
	}
	if err := h.fetcher.GetTxs(ctx, b.TxIDs); err != nil {
		return fmt.Errorf("block get TXs: %w", err)
	}
	return nil
}
