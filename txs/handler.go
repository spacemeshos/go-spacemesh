package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var errDuplicateTX = errors.New("tx already exists")

// TxHandler handles the transactions received via gossip or sync.
type TxHandler struct {
	logger log.Log
	state  conservativeState
}

// NewTxHandler returns a new TxHandler.
func NewTxHandler(s conservativeState, l log.Log) *TxHandler {
	return &TxHandler{
		logger: l,
		state:  s,
	}
}

// HandleGossipTransaction handles data received on the transactions gossip channel.
func (th *TxHandler) HandleGossipTransaction(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	if err := th.handleTransaction(ctx, msg); err != nil {
		th.logger.WithContext(ctx).With().Warning("failed to handle tx", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

// HandleProposalTransaction handles data received on the transactions synced as a part of proposal.
func (th *TxHandler) HandleProposalTransaction(ctx context.Context, msg []byte) error {
	err := th.handleTransaction(ctx, msg)
	if err == nil || errors.Is(err, errDuplicateTX) {
		return nil
	}
	return err
}

func (th *TxHandler) handleTransaction(ctx context.Context, msg []byte) error {
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
		return fmt.Errorf("failed to parse %s: %w", raw.ID, err)
	}
	if !req.Verify() {
		return fmt.Errorf("failed to verify %s", raw.ID)
	}
	if err := th.state.AddToCache(&types.Transaction{RawTx: raw, TxHeader: header}); err != nil {
		th.logger.WithContext(ctx).With().Warning("failed to add tx to conservative cache",
			raw.ID,
			log.Err(err))
		return err
	}
	return nil
}

// HandleBlockTransaction handles transactions received as a reference to a block.
func (th *TxHandler) HandleBlockTransaction(ctx context.Context, data []byte) error {
	raw := types.NewRawTx(data)
	exists, err := th.state.HasTx(raw.ID)
	if err != nil {
		return fmt.Errorf("has sync tx: %w", err)
	} else if exists {
		return nil
	}
	err = th.state.AddToDB(&types.Transaction{RawTx: raw})
	if err != nil {
		return fmt.Errorf("add tx %w", err)
	}
	return nil
}
