package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// IncomingTxProtocol is the protocol identifier for tx received by gossip that is used by the p2p.
const IncomingTxProtocol = "TxGossip"

var (
	errMalformedMsg      = errors.New("malformed tx")
	errAddrNotExtracted  = errors.New("address not extracted")
	errAddrNotFound      = errors.New("address not found")
	errRejectedByMemPool = errors.New("rejected by mempool")
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
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

func (th *TxHandler) handleTransaction(ctx context.Context, msg []byte) error {
	tx, err := types.BytesToTransaction(msg)
	if err != nil {
		th.logger.WithContext(ctx).With().Error("failed to parse tx", log.Err(err))
		return errMalformedMsg
	}

	if err = tx.CalcAndSetOrigin(); err != nil {
		th.logger.WithContext(ctx).Error("failed to calculate tx origin", tx.ID(), log.Err(err))
		return errAddrNotExtracted
	}

	th.logger.WithContext(ctx).Info("got new tx",
		tx.ID(),
		log.Uint64("nonce", tx.AccountNonce),
		log.Uint64("amount", tx.Amount),
		log.Uint64("fee", tx.GetFee()),
		log.Uint64("gas", tx.GasLimit),
		log.String("recipient", tx.GetRecipient().String()),
		log.String("origin", tx.Origin().String()))

	if !th.state.AddressExists(tx.Origin()) {
		th.logger.WithContext(ctx).Error("tx origin does not exist",
			log.String("transaction", tx.String()),
			tx.ID(),
			log.String("origin", tx.Origin().Short()))
		return errAddrNotFound
	}

	if err := th.state.AddTxToMemPool(tx, true); err != nil {
		th.logger.WithContext(ctx).Warning("failed to add tx to mempool", tx.ID(), log.Err(err))
		return errRejectedByMemPool
	}

	return nil
}

// HandleSyncTransaction handles transactions received via sync.
// Unlike HandleGossipTransaction, which only stores valid transactions,
// HandleSyncTransaction only deserializes transactions and stores them regardless of validity. This is because
// transactions received via sync are necessarily referenced somewhere meaning that we must have them stored, even if
// they're invalid, for the data availability of the referencing block.
func (th *TxHandler) HandleSyncTransaction(ctx context.Context, data []byte) error {
	var tx types.Transaction
	err := types.BytesToInterface(data, &tx)
	if err != nil {
		th.logger.WithContext(ctx).Error("failed to parse sync tx", log.Err(err))
		return errMalformedMsg
	}
	if err = tx.CalcAndSetOrigin(); err != nil {
		th.logger.WithContext(ctx).Error("failed to calculate sync tx origin", tx.ID(), log.Err(err))
		return errAddrNotExtracted
	}
	if err := th.state.AddTxToMemPool(&tx, false); err != nil {
		th.logger.WithContext(ctx).Error("failed to add sync tx to mempool", tx.ID(), log.Err(err))
		return fmt.Errorf("add sync tx to mempool: %w", err)
	}
	return nil
}
