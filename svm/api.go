package svm

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/svm/state"
)

// SVM is an entry point for all SVM operations.
type SVM struct {
	*state.TransactionProcessor
	log log.Logger
}

// New creates a new `SVM` instance from the given `state` and `logger`.
func New(state *state.TransactionProcessor, logger log.Log) *SVM {
	return &SVM{state, log.NewDefault("svm")}
}

// SetupGenesis creates new accounts and adds balances as dictated by `conf`.
func (svm *SVM) SetupGenesis(conf *config.GenesisConfig) error {
	if conf == nil {
		conf = config.DefaultGenesisConfig()
	}
	for id, balance := range conf.Accounts {
		bytes := util.FromHex(id)
		if len(bytes) == 0 {
			return fmt.Errorf("cannot decode entry %s for genesis account", id)
		}
		// just make it explicit that we want address and not a public key
		if len(bytes) != types.AddressLength {
			return fmt.Errorf("%s must be an address of size %d", id, types.AddressLength)
		}
		addr := types.BytesToAddress(bytes)
		svm.TransactionProcessor.CreateAccount(addr)
		svm.TransactionProcessor.AddBalance(addr, balance)
		svm.log.With().Info("genesis account created",
			log.String("address", addr.Hex()),
			log.Uint64("balance", balance))
	}

	_, err := svm.TransactionProcessor.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

// ApplyLayer applies the given rewards to some miners as well as a vector of
// transactions for the given layer. to miners vector for layer. It returns an
// error on failure, as well as a vector of failed transactions.
func (svm *SVM) ApplyLayer(layerID types.LayerID, transactions []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	svm.TransactionProcessor.ApplyRewards(layerID, rewards)
	failedTxs, err := svm.TransactionProcessor.ApplyTransactions(layerID, transactions)
	if err != nil {
		return failedTxs, fmt.Errorf("failed applying transactions: %w", err)
	}

	return failedTxs, nil
}
