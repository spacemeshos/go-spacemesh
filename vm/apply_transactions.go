package vm

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	errInvalid = errors.New("invalid tx")
	errFunds   = fmt.Errorf("%w: insufficient funds", errInvalid)
	errNonce   = fmt.Errorf("%w: incorrect nonce", errInvalid)
)

// applyTransaction applies provided transaction to the current state, but does not commit it to persistent
// storage. It returns an error if the transaction is invalid, i.e., if there is not enough balance in the source
// account to perform the transaction and pay the fee or if the nonce is incorrect.
func applyTransaction(logger log.Log, ch *changes, tx *types.Transaction) error {
	balance, err := ch.balance(tx.Origin())
	if err != nil {
		return err
	}
	if total := tx.GetFee() + tx.Amount; balance <= total {
		logger.With().Error("not enough funds",
			log.Uint64("balance_have", balance),
			log.Uint64("balance_need", total))
		return errFunds
	}
	nonce, err := ch.nonce(tx.Origin())
	if err != nil {
		return err
	}
	if correct := nonce + 1; correct != tx.AccountNonce {
		logger.With().Warning("invalid nonce",
			log.Uint64("nonce_correct", correct),
			log.Uint64("nonce_actual", tx.AccountNonce))
		return errNonce
	}
	if err := ch.setNonce(tx.Origin(), tx.AccountNonce); err != nil {
		return err
	}
	if err := ch.addBalance(tx.GetRecipient(), tx.Amount); err != nil {
		return err
	}
	if err := ch.subBalance(tx.Origin(), tx.Amount); err != nil {
		return err
	}
	if err := ch.subBalance(tx.Origin(), tx.GetFee()); err != nil {
		return err
	}
	logger.With().Info("transaction processed", log.Stringer("transaction", tx))
	return nil
}
