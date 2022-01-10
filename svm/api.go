package svm

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-svm/svm"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mempool"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/svm/state"
)

// TODO(nkryuchkov):
// The line below ensures that project may be built when github.com/spacemeshos/go-svm/svm is imported.
// It needs to be removed after github.com/spacemeshos/go-svm/svm is integrated.
var _ svm.API

// IncomingTxProtocol is the protocol identifier for tx received by gossip that is used by the p2p.
const IncomingTxProtocol = state.IncomingTxProtocol

// SVM is an entry point for all SVM operations.
type SVM struct {
	state *state.TransactionProcessor
	log   log.Logger
}

// New creates a new `SVM` instance from the given `state` and `logger`.
func New(allStates, processorDb database.Database, projector state.Projector, txPool *mempool.TxMempool, logger log.Log) *SVM {
	state := state.NewTransactionProcessor(allStates, processorDb, projector, txPool, logger)
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

// Rewind loads the given layer state from persistent storage. On success, it
// also returns the current state root hash *after* rewinding.
func (svm *SVM) Rewind(layer types.LayerID) (types.Hash32, error) {
	err := svm.state.LoadState(layer)
	if err != nil {
		return types.Hash32{}, fmt.Errorf("SVM couldn't rewind back to layer %d: %w", layer.Uint32(), err)
	}
	return svm.state.GetStateRoot(), err
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

// AddTxToPool adds the provided transaction to the transaction pool. The caller
// is responsible for validating tx beforehand with ValidateNonceAndBalance.
func (svm *SVM) AddTxToPool(tx *types.Transaction) error {
	svm.state.AddTxToPool(tx)
	return nil
}

// HandleGossipTransaction handles data received on the transactions gossip channel.
func (svm *SVM) HandleGossipTransaction(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	tx, err := types.BytesToTransaction(msg)
	if err != nil {
		svm.state.With().Error("SVM couldn't parse incoming transaction", log.Err(err))
		return pubsub.ValidationIgnore
	}

	if err := tx.CalcAndSetOrigin(); err != nil {
		svm.state.With().Error("SVM failed to calculate transaction origin", tx.ID(), log.Err(err))
		return pubsub.ValidationIgnore
	}

	svm.log.With().Debug("got new tx",
		tx.ID(),
		log.Uint64("nonce", tx.AccountNonce),
		log.Uint64("amount", tx.Amount),
		log.Uint64("fee", tx.GetFee()),
		log.Uint64("gas", tx.GasLimit),
		log.String("recipient", string(tx.GetRecipient().String())),
		log.String("origin", tx.Origin().String()))

	if !svm.AddressExists(tx.Origin()) {
		svm.state.With().Error("transaction origin does not exist",
			log.String("transaction", tx.String()),
			tx.ID(),
			log.String("origin", tx.Origin().Short()))
		return pubsub.ValidationIgnore
	}

	if err := svm.ValidateNonceAndBalance(tx); err != nil {
		svm.state.With().Error("SVM couldn't validate tx before adding it to the mempool", tx.ID(), log.Err(err))
		return pubsub.ValidationIgnore
	} else if err := svm.AddTxToPool(tx); err != nil {
		svm.state.With().Error("SVM couldn't add tx to mempool", tx.ID(), log.Err(err))
		return pubsub.ValidationIgnore
	}

	return pubsub.ValidationAccept
}

// HandleSyncTransaction handles transactions received via sync.
// Unlike HandleGossipTransaction, which only stores valid transactions,
// HandleSyncTransaction only deserializes transactions and stores them regardless of validity. This is because
// transactions received via sync are necessarily referenced somewhere meaning that we must have them stored, even if
// they're invalid, for the data availability of the referencing block.
func (svm *SVM) HandleSyncTransaction(data []byte) error {
	var tx mesh.DbTransaction
	err := types.BytesToInterface(data, &tx)
	if err != nil {
		svm.state.With().Error("SVM couldn't parse incoming transaction", log.Err(err))
		return fmt.Errorf("parse: %w", err)
	}
	if err = tx.CalcAndSetOrigin(); err != nil {
		return fmt.Errorf("calculate and set origin: %w", err)
	}
	svm.state.AddTxToPool(tx.Transaction)
	return nil
}
