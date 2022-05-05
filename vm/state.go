package vm

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
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

// state is the struct containing state db and is responsible for applying changes to the state.
type state struct {
	logger log.Log
	db     *sql.Database
}

// GetAppliedLayer returns layer of the applied transaction.
func (st *state) GetAppliedLayer(tid types.TransactionID) (types.LayerID, error) {
	return transactions.GetAppliedLayer(st.db, tid)
}

// GetStateRoot returns latest state root.
func (st *state) GetStateRoot() (types.Hash32, error) {
	return layers.GetLatestStateRoot(st.db)
}

// GetLayerStateRoot returns state root for the layer.
func (st *state) GetLayerStateRoot(lid types.LayerID) (types.Hash32, error) {
	return layers.GetStateRoot(st.db, lid)
}

// Account returns latest valid account data.
func (st *state) Account(address types.Address) (types.Account, error) {
	return accounts.Latest(st.db, address)
}

// AddressExists checks if an account address exists in this node's global state.
func (st *state) AddressExists(address types.Address) (bool, error) {
	latest, err := accounts.Latest(st.db, address)
	if err != nil {
		return false, err
	}
	return latest.Layer.Value != 0, nil
}

// Revert state after the layer.
func (st *state) Revert(lid types.LayerID) error {
	return accounts.Revert(st.db, lid)
}

// ApplyGenesis applies genesis config.
func (st *state) ApplyGenesis(genesis *config.GenesisConfig) error {
	ss := newChanges(st.db, types.LayerID{})
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

	if _, err := ss.commit(); err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

func (st *state) GetAllAccounts() ([]*types.Account, error) {
	return accounts.All(st.db)
}

// Apply layer with rewards and transactions.
func (st *state) Apply(lid types.LayerID, rewards []types.AnyReward, txs []*types.Transaction) (types.Hash32, []*types.Transaction, error) {
	ch := newChanges(st.db, lid)
	for _, reward := range rewards {
		if err := ch.addBalance(reward.Address, reward.Amount); err != nil {
			return types.Hash32{}, nil, err
		}
		events.ReportAccountUpdate(reward.Address)
	}
	var failed []*types.Transaction
	for _, tx := range txs {
		if err := applyTransaction(st.logger, ch, tx); err != nil {
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
	hash, err := ch.commit()
	if err != nil {
		return types.Hash32{}, nil, err
	}
	return hash, failed, nil
}
