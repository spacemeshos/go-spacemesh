package vm

import (
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/vm/state"
)

// VM is an entry point for all VM operations.
type VM struct {
	state *state.State
	log   log.Logger
}

// New creates a new `SVM` instance from the given `state` and `logger`.
func New(logger *log.Log, db *sql.Database) *VM {
	return &VM{
		state: state.New(logger, db),
		log:   logger,
	}
}

// SetupGenesis creates new accounts and adds balances as dictated by `conf`.
func (svm *VM) SetupGenesis(conf *config.GenesisConfig) error {
	if conf == nil {
		conf = config.DefaultGenesisConfig()
	}
	return svm.state.ApplyGenesis(conf)
}

// ApplyLayer applies the given rewards to some miners as well as a vector of
// transactions for the given layer. to miners vector for layer. It returns an
// error on failure, as well as a vector of failed transactions.
func (svm *VM) ApplyLayer(lid types.LayerID, transactions []*types.Transaction, rewards []types.AnyReward) ([]*types.Transaction, error) {
	_, failed, err := svm.state.Apply(lid, rewards, transactions)
	return failed, err
}

// GetLayerStateRoot returns the state root at a given layer.
func (svm *VM) GetLayerStateRoot(lid types.LayerID) (types.Hash32, error) {
	return svm.state.GetLayerStateRoot(lid)
}

// GetStateRoot gets the current state root hash.
func (svm *VM) GetStateRoot() (types.Hash32, error) {
	return svm.state.GetStateRoot()
}

// Revert loads the given layer state from persistent storage. On success, it
// also returns the current state root hash *after* rewinding.
func (svm *VM) Revert(lid types.LayerID) (types.Hash32, error) {
	err := svm.state.Revert(lid)
	if err != nil {
		return types.Hash32{}, err
	}
	return types.Hash32{}, err
}

// AddressExists checks if an account address exists in this node's global state.
func (svm *VM) AddressExists(addr types.Address) (bool, error) {
	return svm.state.AddressExists(addr)
}

// GetBalance Retrieve the balance from the given address or 0 if object not found.
func (svm *VM) GetBalance(addr types.Address) (uint64, error) {
	acc, err := svm.state.Account(addr)
	if err != nil {
		return 0, err
	}
	return acc.Balance, nil
}

// GetNonce gets the current nonce of the given addr, if the address is not
// found it returns 0.
func (svm *VM) GetNonce(addr types.Address) (uint64, error) {
	acc, err := svm.state.Account(addr)
	if err != nil {
		return 0, err
	}
	return acc.Nonce, nil
}

// GetAllAccounts returns a dump of all accounts in global state.
func (svm *VM) GetAllAccounts() ([]*types.Account, error) {
	return nil, nil
}
