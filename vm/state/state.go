package state

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

// State is the struct containing state db and is responsible for applying changes to the state.
type State struct {
	logger *log.Log
	db     *sql.Database
}

// New returns a new state processor.
func New(logger *log.Log, db *sql.Database) *State {
	return &State{
		logger: logger,
		db:     db,
	}
}

// Account returns latest valid account data.
func (st *State) Account(address types.Address) (types.Account, error) {
	return accounts.Latest(st.db, address)
}

// AddressExists checks if an account address exists in this node's global state.
func (st *State) AddressExists(address types.Address) (bool, error) {
	latest, err := accounts.Latest(st.db, address)
	if err != nil {
		return false, err
	}
	return latest.Layer.Value != 0, nil
}

// GetLayerApplied gets the layer id at which this tx was applied.
func (st *State) GetLayerApplied(txID types.TransactionID) (types.LayerID, error) {
	return layers.GetLastApplied(st.db)
}

// Revert state after the layer.
func (st *State) Revert(lid types.LayerID) error {
	return accounts.Revert(st.db, lid)
}

// ApplyGenesis applies genesis config.
func (st *State) ApplyGenesis(genesis *config.GenesisConfig) error {
	ss := newStagedState(st.db, types.LayerID{})
	for id, balance := range genesis.Accounts {
		bytes := util.FromHex(id)
		if len(bytes) == 0 {
			return fmt.Errorf("cannot decode entry %s for genesis account", id)
		}
		// just make it explicit that we want address and not a public key
		if len(bytes) != types.AddressLength {
			return fmt.Errorf("%s must be an address of size %d", id, types.AddressLength)
		}
		addr := types.BytesToAddress(bytes)
		ss.addBalance(addr, balance)
		st.logger.With().Info("genesis account created",
			log.Stringer("address", addr),
			log.Uint64("balance", balance))
	}

	if err := ss.commit(); err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

// Apply layer with rewards and transactions.
func (st *State) Apply(lid types.LayerID, rewards []*types.Reward, txs []*types.Transaction) (types.Hash32, []*types.Transaction, error) {
	ss := newStagedState(st.db, lid)
	for _, reward := range rewards {
		if err := ss.addBalance(reward.Coinbase, reward.TotalReward); err != nil {
			return types.Hash32{}, nil, err
		}
		events.ReportAccountUpdate(reward.Coinbase)
	}
	var failed []*types.Transaction
	for _, tx := range txs {
		if err := applyTransaction(st.logger, ss, tx); err != nil {
			if errors.Is(err, errInvalid) {
				failed = append(failed, tx)
			} else {
				return types.Hash32{}, nil, err
			}
		}
		events.ReportNewTx(lid, tx)
		events.ReportAccountUpdate(tx.Origin())
		events.ReportAccountUpdate(tx.GetRecipient())
	}
	if err := ss.commit(); err != nil {
		return types.Hash32{}, nil, err
	}
	return types.Hash32{}, failed, nil
}
