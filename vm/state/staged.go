package state

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func newStagedState(db *sql.Database, layer types.LayerID) *stagedState {
	return &stagedState{
		db:      db,
		changed: map[types.Address]*types.Account{},
		layer:   layer,
	}
}

type stagedState struct {
	db *sql.Database

	layer   types.LayerID
	order   list.List
	changed map[types.Address]*types.Account
}

func (s *stagedState) loadAccount(address types.Address) (*types.Account, error) {
	if acc, exist := s.changed[address]; exist {
		return acc, nil
	}
	acc, err := accounts.Latest(s.db, address)
	if err != nil {
		return nil, err
	}
	s.order.PushBack(address)
	s.changed[address] = &acc
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

func (s *stagedState) commit() (types.Hash32, error) {
	tx, err := s.db.Tx(context.Background())
	if err != nil {
		return types.Hash32{}, err
	}
	defer tx.Release()
	hasher := sha256.New()
	buf := [8]byte{}
	for elem := s.order.Front(); elem != nil; elem = elem.Next() {
		account := s.changed[elem.Value.(types.Address)]
		if err := accounts.Update(tx, account); err != nil {
			return types.Hash32{}, err
		}
		binary.LittleEndian.PutUint64(buf[:], account.Balance)
		hasher.Write(buf[:])
	}
	var hash types.Hash32
	hasher.Sum(hash[:0])
	if err := layers.UpdateStateRoot(tx, s.layer, hash); err != nil {
		return types.Hash32{}, err
	}

	if err := tx.Commit(); err != nil {
		return types.Hash32{}, err
	}

	return hash, nil
}
