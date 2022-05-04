package state

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	errInvalid = errors.New("invalid tx")
	errOrigin  = fmt.Errorf("%w: origin account doesnt exist", errInvalid)
	errFunds   = fmt.Errorf("%w: insufficient funds", errInvalid)
	errNonce   = fmt.Errorf("%w: incorrect nonce", errInvalid)
)

// applyTransaction applies provided transaction to the current state, but does not commit it to persistent
// storage. It returns an error if the transaction is invalid, i.e., if there is not enough balance in the source
// account to perform the transaction and pay the fee or if the nonce is incorrect.
func applyTransaction(logger *log.Log, ss *stagedState, tx *types.Transaction) error {
	balance, err := ss.balance(tx.Origin())
	if err != nil {
		return err
	}

	amountWithFee := tx.GetFee() + tx.Amount

	// todo: should we allow to spend all accounts balance?
	if balance <= amountWithFee {
		logger.With().Error("not enough funds",
			log.Uint64("balance_have", balance),
			log.Uint64("balance_need", amountWithFee))
		return errFunds
	}
	nonce, err := ss.nonce(tx.Origin())
	if err != nil {
		return err
	}
	if correct := nonce + 1; correct != tx.AccountNonce {
		logger.With().Warning("invalid nonce",
			log.Uint64("nonce_correct", correct),
			log.Uint64("nonce_actual", tx.AccountNonce))
		return errNonce
	}

	if err := ss.setNonce(tx.Origin(), tx.AccountNonce); err != nil {
		return err
	}
	if err := ss.addBalance(tx.GetRecipient(), tx.Amount); err != nil {
		return err
	}
	if err := ss.subBalance(tx.Origin(), tx.Amount); err != nil {
		return err
	}
	if err := ss.subBalance(tx.Origin(), tx.GetFee()); err != nil {
		return err
	}
	logger.With().Info("transaction processed", log.String("transaction", tx.String()))
	return nil
}
