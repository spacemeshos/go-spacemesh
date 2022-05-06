package vm

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

type changedAccount struct {
	types.Account
	dirty bool
}

func newChanges(db *sql.Database, layer types.LayerID) *changes {
	return &changes{
		db:      db,
		changed: map[types.Address]*changedAccount{},
		layer:   layer,
	}
}

type changes struct {
	db *sql.Database

	layer   types.LayerID
	order   list.List
	changed map[types.Address]*changedAccount
}

func (s *changes) loadAccount(address types.Address) (*changedAccount, error) {
	if chacc, exist := s.changed[address]; exist {
		return chacc, nil
	}
	acc, err := accounts.Latest(s.db, address)
	if err != nil {
		return nil, err
	}
	s.order.PushBack(address)
	acc.Layer = s.layer
	chacc := &changedAccount{Account: acc}
	s.changed[address] = chacc
	return chacc, nil
}

func (s *changes) nonce(address types.Address) (uint64, error) {
	acc, err := s.loadAccount(address)
	if err != nil {
		return 0, err
	}
	return acc.Nonce, nil
}

func (s *changes) setNonce(address types.Address, nonce uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.dirty = true
	acc.Nonce = nonce
	return nil
}

func (s *changes) balance(address types.Address) (uint64, error) {
	acc, err := s.loadAccount(address)
	if err != nil {
		return 0, err
	}
	return acc.Balance, nil
}

func (s *changes) addBalance(address types.Address, value uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.dirty = true
	acc.Balance += value
	return nil
}

func (s *changes) subBalance(address types.Address, value uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.dirty = true
	acc.Balance -= value
	return nil
}

func (s *changes) commit() (types.Hash32, error) {
	tx, err := s.db.Tx(context.Background())
	if err != nil {
		return types.Hash32{}, err
	}
	defer tx.Release()
	hasher := sha256.New()
	buf := [8]byte{}
	for elem := s.order.Front(); elem != nil; elem = elem.Next() {
		account := s.changed[elem.Value.(types.Address)]
		if !account.dirty {
			continue
		}
		if err := accounts.Update(tx, &account.Account); err != nil {
			return types.Hash32{}, err
		}
		binary.LittleEndian.PutUint64(buf[:], account.Balance)
		hasher.Write(buf[:])
	}

	var hash types.Hash32
	hasher.Sum(hash[:0])
	if err := layers.UpdateStateHash(tx, s.layer, hash); err != nil {
		return types.Hash32{}, err
	}

	if err := tx.Commit(); err != nil {
		return types.Hash32{}, err
	}
	return hash, nil
}
