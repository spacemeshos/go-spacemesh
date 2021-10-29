package svm

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/svm/state"
)

// SVM is an entry point for all SVM operations.
type SVM struct {
	state *state.TransactionProcessor
	log   log.Logger
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
		svm.state.CreateAccount(addr)
		svm.state.AddBalance(addr, balance)
		svm.log.With().Info("genesis account created",
			log.String("address", addr.Hex()),
			log.Uint64("balance", balance))
	}

	_, err := svm.state.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

// ApplyLayer applies the given rewards to some miners as well as a vector of
// transactions for the given layer. to miners vector for layer. It returns an
// error on failure, as well as a vector of failed transactions.
func (svm *SVM) ApplyLayer(layerID types.LayerID, transactions []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	svm.state.ApplyRewards(layerID, rewards)
	failedTxs, err := svm.state.ApplyTransactions(layerID, transactions)
	if err != nil {
		return failedTxs, fmt.Errorf("SVM couldn't apply layer %d: %w", layerID.Uint32(), err)
	}

	return failedTxs, nil
}

// AddressExists checks if an account address exists in this node's global state.
func (svm *SVM) AddressExists(addr types.Address) bool {
	return svm.state.AddressExists(addr)
}

// GetLayerApplied gets the layer id at which this tx was applied.
func (svm *SVM) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	return svm.state.GetLayerApplied(txID)
}

// GetLayerStateRoot returns the state root at a given layer.
func (svm *SVM) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	hash, err := svm.state.GetLayerStateRoot(layer)
	if err != nil {
		err = fmt.Errorf("SVM couldn't get the root hash of layer %d: %w", layer.Uint32(), err)
	}
	return hash, err
}

// GetStateRoot gets the current state root hash.
func (svm *SVM) GetStateRoot() types.Hash32 {
	return svm.state.GetStateRoot()
}

// LoadState loads the last state from persistent storage.
func (svm *SVM) LoadState(layer types.LayerID) error {
	if err := svm.state.LoadState(layer); err != nil {
		return fmt.Errorf("SVM couldn't recover the state at layer %d: %w", layer.Uint32(), err)
	}
	return nil
}

// GetBalance Retrieve the balance from the given address or 0 if object not found.
func (svm *SVM) GetBalance(addr types.Address) uint64 {
	return svm.state.GetBalance(addr)
}

// GetNonce gets the current nonce of the given addr, if the address is not
// found it returns 0.
func (svm *SVM) GetNonce(addr types.Address) uint64 {
	return svm.state.GetNonce(addr)
}

// GetAllAccounts returns a dump of all accounts in global state.
func (svm *SVM) GetAllAccounts() (*types.MultipleAccountsState, error) {
	accounts, err := svm.state.GetAllAccounts()
	if err != nil {
		err = fmt.Errorf("SVM couldn't get all accounts: %w", err)
	}
	return accounts, err
}

// ValidateNonceAndBalance validates that the tx origin account has enough balance to apply the tx,
// also, it checks that nonce in tx is correct, returns error otherwise.
func (svm *SVM) ValidateNonceAndBalance(transaction *types.Transaction) error {
	if err := svm.state.ValidateNonceAndBalance(transaction); err != nil {
		return fmt.Errorf("SVM couldn't validate nonce and balance: %w", err)
	}
	return nil
}

// ValidateAndAddTxToPool validates the provided tx nonce and balance with projector and puts it in the transaction pool
// it returns an error if the provided tx is not valid.
//
// TODO: Remove this and use a whole separate API for mempool management.
func (svm *SVM) ValidateAndAddTxToPool(tx *types.Transaction) error {
	if err := svm.state.ValidateAndAddTxToPool(tx); err != nil {
		return fmt.Errorf("SVM couldn't validate and/or add transaction to mempool: %w", err)
	}
	return nil
}

// HandleGossipTransaction wraps around HandleTransaction,
// which handles data received on the transactions gossip channel.
func (svm *SVM) HandleGossipTransaction(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	if err := svm.HandleTransaction(msg); err != nil {
		svm.state.With().Error("invalid transaction", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

// HandleTransaction handles data received on transactions gossip channel.
func (svm *SVM) HandleTransaction(data []byte) error {
	tx, err := types.BytesToTransaction(data)
	if err != nil {
		svm.state.With().Error("cannot parse incoming transaction", log.Err(err))
		return fmt.Errorf("parse: %w", err)
	}
	if err := types.BytesToInterface(data, &tx); err != nil {
		svm.state.With().Error("cannot parse incoming transaction data", log.Err(err))
		return fmt.Errorf("parse: %w", err)
	}

	if err := svm.handleTransaction(tx); err != nil {
		return fmt.Errorf("handle transaction: %w", err)
	}

	return nil
}

func (svm *SVM) handleTransaction(tx *types.Transaction) error {
	if err := tx.CalcAndSetOrigin(); err != nil {
		svm.state.With().Error("failed to calculate transaction origin", tx.ID(), log.Err(err))
		return fmt.Errorf("calculate and set origin: %w", err)
	}

	svm.state.Log.With().Info("got new tx",
		tx.ID(),
		log.Uint64("nonce", tx.AccountNonce),
		log.Uint64("amount", tx.Amount),
		log.Uint64("fee", tx.Fee),
		log.Uint64("gas", tx.GasLimit),
		log.String("recipient", tx.Recipient.String()),
		log.String("origin", tx.Origin().String()))

	if !svm.state.AddressExists(tx.Origin()) {
		svm.state.With().Error("transaction origin does not exist",
			log.String("transaction", tx.String()),
			tx.ID(),
			log.String("origin", tx.Origin().Short()))
		return fmt.Errorf("transaction origin does not exist")
	}
	if err := svm.ValidateNonceAndBalance(tx); err != nil {
		svm.state.With().Error("nonce and balance validation failed", tx.ID(), log.Err(err))
		return fmt.Errorf("nonce and balance validation failed")
	}

	svm.ValidateAndAddTxToPool(tx)
	return nil
}
