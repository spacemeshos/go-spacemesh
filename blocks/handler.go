package blocks

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errMalformedData  = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errInvalidRewards = errors.New("invalid rewards")
	errDuplicateTX    = errors.New("duplicate TxID in proposal")
)

// Handler processes Block fetched from peers during sync.
type Handler struct {
	logger log.Log

	fetcher system.Fetcher
	db      *sql.Database
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
func NewHandler(f system.Fetcher, db *sql.Database, m meshProvider, opts ...Opt) *Handler {
	h := &Handler{
		logger:  log.NewNop(),
		fetcher: f,
		db:      db,
		mesh:    m,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleSyncedBlock handles Block data from sync.
func (h *Handler) HandleSyncedBlock(ctx context.Context, peer p2p.Peer, data []byte) error {
	logger := h.logger.WithContext(ctx)

	var b types.Block
	if err := codec.Decode(data, &b); err != nil {
		logger.With().Error("malformed block", log.Err(err))
		return errMalformedData
	}
	// set the block ID when received
	b.Initialize()
	if b.LayerIndex <= types.GetEffectiveGenesis() {
		return fmt.Errorf("block before effective genesis: layer %v", b.LayerIndex)
	}

	if err := ValidateRewards(b.Rewards); err != nil {
		return fmt.Errorf("%w: %s", errInvalidRewards, err.Error())
	}

	logger = logger.WithFields(b.ID(), b.LayerIndex)

	if exists, err := blocks.Has(h.db, b.ID()); err != nil {
		logger.With().Error("failed to check block exist", log.Err(err))
	} else if exists {
		logger.Debug("known block")
		return nil
	}
	logger.With().Info("new block")

	h.fetcher.RegisterPeerHashes(peer, types.TransactionIDsToHashes(b.TxIDs))
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

// ValidateRewards syntactically validates rewards.
func ValidateRewards(rewards []types.AnyReward) error {
	if len(rewards) == 0 {
		return fmt.Errorf("empty rewards")
	}
	unique := map[types.ATXID]struct{}{}
	for _, reward := range rewards {
		if reward.Weight.Num == 0 || reward.Weight.Denom == 0 {
			return fmt.Errorf("reward with invalid (zeroed) weight (%d/%d) included into the block for %v", reward.Weight.Num, reward.Weight.Denom, reward.AtxID)
		}
		if _, exists := unique[reward.AtxID]; exists {
			return fmt.Errorf("multiple rewards for the same atx %v", reward.AtxID)
		}
		unique[reward.AtxID] = struct{}{}
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
	if err := h.fetcher.GetBlockTxs(ctx, b.TxIDs); err != nil {
		return fmt.Errorf("block get TXs: %w", err)
	}
	return nil
}
