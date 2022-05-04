package state

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

func newStagedState(db *sql.Database, layer types.LayerID) *stagedState {
	return &stagedState{
		db:    db,
		state: map[types.Address]*types.Account{},
		layer: layer,
	}
}

type stagedState struct {
	db *sql.Database

	layer types.LayerID
	state map[types.Address]*types.Account
}

func (s *stagedState) loadAccount(address types.Address) (*types.Account, error) {
	if acc, exist := s.state[address]; exist {
		return acc, nil
	}
	acc, err := accounts.Latest(s.db, address)
	if err != nil {
		return nil, err
	}
	s.state[address] = &acc
	return &acc, nil
}

func (s *stagedState) nonce(address types.Address) (uint64, error) {
	acc, err := s.loadAccount(address)
	if err != nil {
		return 0, err
	}
	return acc.Nonce, nil
}

func (s *stagedState) setNonce(address types.Address, nonce uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.Nonce = nonce
	return nil
}

func (s *stagedState) balance(address types.Address) (uint64, error) {
	acc, err := s.loadAccount(address)
	if err != nil {
		return 0, err
	}
	return acc.Balance, nil
}

func (s *stagedState) addBalance(address types.Address, value uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.Balance += value
	acc.Layer = s.layer
	return nil
}

func (s *stagedState) subBalance(address types.Address, value uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.Balance -= value
	acc.Layer = s.layer
	return nil
}

func (s *stagedState) commit() error {
	tx, err := s.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer tx.Release()
	for _, account := range s.state {
		if err := accounts.Update(tx, account); err != nil {
			return err
		}
	}
	return tx.Commit()
}
