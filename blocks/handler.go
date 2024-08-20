package blocks

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

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
	errMalformedData = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errWrongHash     = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errDuplicateTX   = errors.New("duplicate TxID in proposal")
)

// Handler processes Block fetched from peers during sync.
type Handler struct {
	logger *zap.Logger

	fetcher  system.Fetcher
	db       sql.StateDatabase
	tortoise tortoiseProvider
	mesh     meshProvider
}

// Opt for configuring BlockHandler.
type Opt func(*Handler)

// WithLogger defines logger for Handler.
func WithLogger(logger *zap.Logger) Opt {
	return func(h *Handler) {
		h.logger = logger
	}
}

// NewHandler creates new Handler.
func NewHandler(
	f system.Fetcher,
	db sql.StateDatabase,
	tortoise tortoiseProvider,
	m meshProvider,
	opts ...Opt,
) *Handler {
	h := &Handler{
		logger:   zap.NewNop(),
		fetcher:  f,
		db:       db,
		tortoise: tortoise,
		mesh:     m,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleSyncedBlock handles Block data from sync.
func (h *Handler) HandleSyncedBlock(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	logger := h.logger.With(log.ZContext(ctx))

	var b types.Block
	if err := codec.Decode(data, &b); err != nil {
		logger.Debug("malformed block", zap.Error(err))
		return errMalformedData
	}
	// set the block ID when received
	b.Initialize()

	if b.ID().AsHash32() != expHash {
		return fmt.Errorf("%w: block want %s, got %s", errWrongHash, expHash.ShortString(), b.ID().String())
	}

	if b.LayerIndex <= types.GetEffectiveGenesis() {
		return fmt.Errorf("%w: block before effective genesis: layer %v", pubsub.ErrValidationReject, b.LayerIndex)
	}

	if err := ValidateRewards(b.Rewards); err != nil {
		return fmt.Errorf("%w: %s", pubsub.ErrValidationReject, err.Error())
	}

	logger = logger.With(zap.Stringer("block_id", b.ID()), zap.Uint32("layer", b.LayerIndex.Uint32()))

	if exists, err := blocks.Has(h.db, b.ID()); err != nil {
		logger.Error("failed to check block exist", zap.Error(err))
	} else if exists {
		logger.Debug("known block")
		return nil
	}
	logger.Debug("new block")

	if missing := h.tortoise.GetMissingActiveSet(b.LayerIndex.GetEpoch(), toAtxIDs(b.Rewards)); len(missing) > 0 {
		h.fetcher.RegisterPeerHashes(peer, types.ATXIDsToHashes(missing))
		if err := h.fetcher.GetAtxs(ctx, missing); err != nil {
			return err
		}
	}
	if len(b.TxIDs) > 0 {
		if err := h.checkTransactions(ctx, peer, &b); err != nil {
			logger.Warn("failed to fetch block TXs", zap.Error(err))
			return err
		}
	}
	if err := h.mesh.AddBlockWithTXs(ctx, &b); err != nil {
		logger.Error("failed to save block", zap.Error(err))
		return fmt.Errorf("save block: %w", err)
	}
	return nil
}

// ValidateRewards syntactically validates rewards.
func ValidateRewards(rewards []types.AnyReward) error {
	if len(rewards) == 0 {
		return errors.New("empty rewards")
	}
	unique := map[types.ATXID]struct{}{}
	for _, reward := range rewards {
		if reward.Weight.Num == 0 || reward.Weight.Denom == 0 {
			return fmt.Errorf(
				"reward with invalid (zeroed) weight (%d/%d) included into the block for %v",
				reward.Weight.Num,
				reward.Weight.Denom,
				reward.AtxID,
			)
		}
		if _, exists := unique[reward.AtxID]; exists {
			return fmt.Errorf("multiple rewards for the same atx %v", reward.AtxID)
		}
		unique[reward.AtxID] = struct{}{}
	}
	return nil
}

func (h *Handler) checkTransactions(ctx context.Context, peer p2p.Peer, b *types.Block) error {
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
	h.fetcher.RegisterPeerHashes(peer, types.TransactionIDsToHashes(b.TxIDs))
	if err := h.fetcher.GetBlockTxs(ctx, b.TxIDs); err != nil {
		return fmt.Errorf("block get TXs: %w", err)
	}
	return nil
}

func toAtxIDs(rewards []types.AnyReward) []types.ATXID {
	rst := make([]types.ATXID, 0, len(rewards))
	for i := range rewards {
		rst = append(rst, rewards[i].AtxID)
	}
	return rst
}
