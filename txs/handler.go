package txs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// IncomingTxProtocol is the protocol identifier for tx received by gossip that is used by the p2p.
const IncomingTxProtocol = "TxGossip"

var (
	errMalformedMsg     = errors.New("malformed tx")
	errDuplicateTX      = errors.New("tx already exists")
	errAddrNotExtracted = errors.New("address not extracted")
	errAddrNotFound     = errors.New("address not found")
)

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

func (th *TxHandler) handleTransaction(ctx context.Context, msg []byte) error {
	raw := types.NewRawTx(msg)
	if exists, err := th.state.HasTx(raw.ID); err != nil {
		return fmt.Errorf("has tx: %w", err)
	} else if exists {
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

	if err := th.state.AddToCache(&types.Transaction{
		RawTx:    raw,
		TxHeader: header,
	}); err != nil {
		th.logger.WithContext(ctx).With().Warning("failed to add tx to conservative cache",
			raw.ID,
			log.Err(err))
	}

	return nil
}

// HandleSyncTransaction handles transactions received via sync.
// Unlike HandleGossipTransaction, which only stores valid transactions,
// HandleSyncTransaction only deserializes transactions and stores them regardless of validity. This is because
// transactions received via sync are necessarily referenced somewhere meaning that we must have them stored, even if
// they're invalid, for the data availability of the referencing block.
func (th *TxHandler) HandleSyncTransaction(ctx context.Context, data []byte) error {
	raw := types.NewRawTx(data)
	exists, err := th.state.HasTx(raw.ID)
	if err != nil {
		th.logger.WithContext(ctx).With().Warning("failed to check sync tx exists", log.Err(err))
		return fmt.Errorf("has sync tx: %w", err)
	} else if exists {
		return nil
	}
	err = th.state.Add(&types.Transaction{RawTx: raw}, time.Now())
	if err != nil {
		th.logger.WithContext(ctx).With().Warning("failed to add transaction", log.Err(err))
		return fmt.Errorf("add tx %w", err)
	}
	return nil
}
