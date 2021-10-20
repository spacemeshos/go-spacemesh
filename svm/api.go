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
	txProcessor *state.TransactionProcessor
	log         log.Logger
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
		svm.txProcessor.CreateAccount(addr)
		svm.txProcessor.AddBalance(addr, balance)
		svm.log.With().Info("genesis account created",
			log.String("address", addr.Hex()),
			log.Uint64("balance", balance))
	}

	_, err := svm.txProcessor.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

// ApplyLayer applies the given rewards to some miners as well as a vector of
// transactions for the given layer. to miners vector for layer. It returns an
// error on failure, as well as a vector of failed transactions.
func (svm *SVM) ApplyLayer(layerID types.LayerID, transactions []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	svm.txProcessor.ApplyRewards(layerID, rewards)
	failedTxs, err := svm.txProcessor.ApplyTransactions(layerID, transactions)
	if err != nil {
		return failedTxs, fmt.Errorf("SVM failed applying transactions: %w", err)
	}

	return failedTxs, nil
}

// AddressExists checks if an account address exists in this node's global state.
func (svm *SVM) AddressExists(addr types.Address) bool {
	return svm.txProcessor.AddressExists(addr)
}

// GetLayerApplied gets the layer id at which this tx was applied.
func (svm *SVM) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	return svm.txProcessor.GetLayerApplied(txID)
}

// GetLayerStateRoot returns the state root at a given layer.
func (svm *SVM) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	hash, err := svm.txProcessor.GetLayerStateRoot(layer)
	if err != nil {
		err = fmt.Errorf("SVM cannot recover the state root of layer %d: %w", layer.Uint32(), err)
	}
	return hash, err
}

// GetStateRoot gets the current state root hash.
func (svm *SVM) GetStateRoot() types.Hash32 {
	return svm.txProcessor.GetStateRoot()
}

// LoadState loads the last state from persistent storage.
func (svm *SVM) LoadState(layer types.LayerID) error {
	if err := svm.txProcessor.LoadState(layer); err != nil {
		return fmt.Errorf("SVM cannot rewind back to layer %d: %w", layer.Uint32(), err)
	}
	return nil
}

// GetBalance Retrieve the balance from the given address or 0 if object not found.
func (svm *SVM) GetBalance(addr types.Address) uint64 {
	return svm.txProcessor.GetBalance(addr)
}

// GetNonce gets the current nonce of the given addr, if the address is not
// found it returns 0.
func (svm *SVM) GetNonce(addr types.Address) uint64 {
	return svm.txProcessor.GetBalance(addr)
}

// GetAllAccounts returns a dump of all accounts in global state.
func (svm *SVM) GetAllAccounts() (*types.MultipleAccountsState, error) {
	accounts, err := svm.txProcessor.GetAllAccounts()
	if err != nil {
		err = fmt.Errorf("SVM couldn't get all accounts: %w", err)
	}
	return accounts, err
}

// ValidateNonceAndBalance validates that the tx origin account has enough balance to apply the tx,
// also, it checks that nonce in tx is correct, returns error otherwise.
func (svm *SVM) ValidateNonceAndBalance(transaction *types.Transaction) error {
	if err := svm.txProcessor.ValidateNonceAndBalance(transaction); err != nil {
		return fmt.Errorf("SVM failed validating nonce and balance: %w", err)
	}
	return nil
}

// ValidateAndAddTxToPool validates the provided tx nonce and balance with projector and puts it in the transaction pool
// it returns an error if the provided tx is not valid.
//
// TODO: Remove this and use a whole separate API for mempool management.
func (svm *SVM) ValidateAndAddTxToPool(tx *types.Transaction) error {
	if err := svm.txProcessor.ValidateAndAddTxToPool(tx); err != nil {
		return fmt.Errorf("SVM cannot validate transaction and/or add it to the mempool: %w", err)
	}
	return nil
}
