package txs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	errWrongHash   = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errDuplicateTX = errors.New("tx already exists")
	errParse       = errors.New("failed to parse tx")
	errVerify      = errors.New("failed to verify tx")
)

// TxHandler handles the transactions received via gossip or sync.
type TxHandler struct {
	self   peer.ID
	logger *zap.Logger
	state  conservativeState
}

// NewTxHandler returns a new TxHandler.
func NewTxHandler(s conservativeState, id peer.ID, l *zap.Logger) *TxHandler {
	return &TxHandler{
		self:   id,
		logger: l,
		state:  s,
	}
}

func updateMetrics(err error, counter *prometheus.CounterVec) {
	switch {
	case err == nil:
		counter.WithLabelValues(saved).Inc()
	case errors.Is(err, errDuplicateTX):
		counter.WithLabelValues(duplicate).Inc()
	case errors.Is(err, errBadNonce):
		counter.WithLabelValues(rejectedBadNonce).Inc()
	case errors.Is(err, errParse):
		counter.WithLabelValues(cantParse).Inc()
	case errors.Is(err, errVerify):
		counter.WithLabelValues(cantVerify).Inc()
	default:
		counter.WithLabelValues(rejectedInternalErr).Inc()
	}
}

// HandleGossipTransaction handles data received on the transactions gossip channel.
func (th *TxHandler) HandleGossipTransaction(ctx context.Context, peer p2p.Peer, msg []byte) error {
	if peer == th.self {
		return nil
	}

	err := th.VerifyAndCacheTx(ctx, msg)
	updateMetrics(err, gossipTxCount)
	if err != nil {
		if !errors.Is(err, errDuplicateTX) {
			th.logger.With(log.ZContext(ctx)).Debug("failed to handle tx", zap.Error(err))
		}
		return err
	}
	return nil
}

// HandleProposalTransaction handles data received on the transactions synced as a part of proposal.
func (th *TxHandler) HandleProposalTransaction(
	ctx context.Context,
	expHash types.Hash32,
	_ p2p.Peer,
	msg []byte,
) error {
	err := th.verifyAndCache(ctx, expHash, msg)
	updateMetrics(err, proposalTxCount)
	if errors.Is(err, errDuplicateTX) {
		return nil
	}
	return err
}

func (th *TxHandler) VerifyAndCacheTx(ctx context.Context, msg []byte) error {
	return th.verifyAndCache(ctx, types.Hash32{}, msg)
}

func (th *TxHandler) verifyAndCache(ctx context.Context, expHash types.Hash32, msg []byte) error {
	raw := types.NewRawTx(msg)
	mtx, err := th.state.GetMeshTransaction(raw.ID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return fmt.Errorf("get tx %w", err)
	}
	if mtx != nil && mtx.TxHeader != nil {
		return errDuplicateTX
	}

	req := th.state.Validation(raw)
	header, err := req.Parse()
	if err != nil {
		return fmt.Errorf("%w: %s (err: %s)", errParse, raw.ID, err)
	}
	tx := &types.Transaction{RawTx: raw, TxHeader: header}
	if expHash != (types.Hash32{}) && tx.ID.Hash32() != expHash {
		return fmt.Errorf("%w: proposal tx want %s, got %s", errWrongHash, expHash.ShortString(), tx.ID.ShortString())
	}
	if header.LayerLimits.Min != 0 || header.LayerLimits.Max != 0 {
		return fmt.Errorf("%w: layers limits are not enabled %s", errParse, raw.ID)
	}
	if header.GasPrice == 0 || header.Fee() == 0 {
		return fmt.Errorf("%w: zero gas price %s", errParse, raw.ID)
	}
	if !req.Verify() {
		return fmt.Errorf("%w: %s", errVerify, raw.ID)
	}
	if err := th.state.AddToCache(ctx, tx, time.Now()); err != nil {
		th.logger.With(log.ZContext(ctx)).Debug("failed to add tx to conservative cache",
			zap.Stringer("tx_id", raw.ID),
			zap.Error(err),
		)
		return err
	}
	return nil
}

// HandleBlockTransaction handles transactions received as a reference to a block.
func (th *TxHandler) HandleBlockTransaction(_ context.Context, expHash types.Hash32, _ p2p.Peer, data []byte) error {
	raw := types.NewRawTx(data)
	exists, err := th.state.HasTx(raw.ID)
	if err != nil {
		blockTxCount.WithLabelValues(rejectedInternalErr).Inc()
		return fmt.Errorf("has block tx: %w", err)
	} else if exists {
		blockTxCount.WithLabelValues(duplicate).Inc()
		return nil
	}
	tx := &types.Transaction{RawTx: raw}
	if tx.ID.Hash32() != expHash {
		return fmt.Errorf("%w: block tx want %s, got %s", errWrongHash, expHash.ShortString(), tx.ID.ShortString())
	}
	req := th.state.Validation(raw)
	header, err := req.Parse()
	if err == nil {
		if req.Verify() {
			tx.TxHeader = header
		} else {
			blockTxCount.WithLabelValues(cantVerify).Inc()
		}
	} else {
		blockTxCount.WithLabelValues(cantParse).Inc()
	}
	if err = th.state.AddToDB(tx); err != nil {
		blockTxCount.WithLabelValues(rejectedInternalErr).Inc()
		return fmt.Errorf("add block tx %w", err)
	}
	if header != nil {
		blockTxCount.WithLabelValues(saved).Inc()
	} else {
		blockTxCount.WithLabelValues(savedNoHdr).Inc()
	}
	return nil
}
