package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	errDuplicateTX = errors.New("tx already exists")
	errParse       = errors.New("failed to parse tx")
	errVerify      = errors.New("failed to verify tx")
)

// TxHandler handles the transactions received via gossip or sync.
type TxHandler struct {
	self   peer.ID
	logger log.Log
	state  conservativeState
}

// NewTxHandler returns a new TxHandler.
func NewTxHandler(s conservativeState, id peer.ID, l log.Log) *TxHandler {
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
		th.logger.WithContext(ctx).With().Warning("failed to handle tx", log.Err(err))
		return err
	}
	return nil
}

// HandleProposalTransaction handles data received on the transactions synced as a part of proposal.
func (th *TxHandler) HandleProposalTransaction(ctx context.Context, _ p2p.Peer, msg []byte) error {
	err := th.VerifyAndCacheTx(ctx, msg)
	updateMetrics(err, proposalTxCount)
	if errors.Is(err, errDuplicateTX) {
		return nil
	}
	return err
}

func (th *TxHandler) VerifyAndCacheTx(ctx context.Context, msg []byte) error {
	raw := types.NewRawTx(msg)
	tx, err := th.state.GetMeshTransaction(raw.ID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return fmt.Errorf("get tx %w", err)
	}
	if tx != nil && tx.TxHeader != nil {
		return errDuplicateTX
	}

	req := th.state.Validation(raw)
	header, err := req.Parse()
	if err != nil {
		return fmt.Errorf("%w: %s (err: %s)", errParse, raw.ID, err)
	}
	if header.GasPrice == 0 {
		return fmt.Errorf("%w: zero gas price %s", errParse, raw.ID)
	}
	if !req.Verify() {
		return fmt.Errorf("%w: %s", errVerify, raw.ID)
	}
	if err := th.state.AddToCache(ctx, &types.Transaction{RawTx: raw, TxHeader: header}); err != nil {
		th.logger.WithContext(ctx).With().Warning("failed to add tx to conservative cache",
			raw.ID,
			log.Err(err),
		)
		return err
	}
	return nil
}

// HandleBlockTransaction handles transactions received as a reference to a block.
func (th *TxHandler) HandleBlockTransaction(_ context.Context, _ p2p.Peer, data []byte) error {
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
