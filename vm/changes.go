package vm

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
)

func newChanges(log log.Log, db *sql.Database, layer types.LayerID) *changes {
	return &changes{
		log:     log,
		db:      db,
		changed: map[types.Address]*types.Account{},
		rewards: make([]*types.Reward, 0),
		layer:   layer,
	}
}

type changes struct {
	log log.Log
	db  *sql.Database

	layer types.LayerID
	// order of changed accounts is necessary for the state consistency.
	order   list.List
	changed map[types.Address]*types.Account
	rewards []*types.Reward
}

func (s *changes) loadAccount(address types.Address) (*types.Account, error) {
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

func (s *changes) nextNonce(address types.Address) (uint64, error) {
	acc, err := s.loadAccount(address)
	if err != nil {
		return 0, err
	}
	if !acc.Initialized {
		return 0, nil
	}
	return acc.Nonce + 1, nil
}

func (s *changes) setNonce(address types.Address, nonce uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.Initialized = true
	acc.Layer = s.layer
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
	acc.Layer = s.layer
	acc.Balance += value
	return nil
}

func (s *changes) subBalance(address types.Address, value uint64) error {
	acc, err := s.loadAccount(address)
	if err != nil {
		return err
	}
	acc.Layer = s.layer
	acc.Balance -= value
	return nil
}

func (s *changes) addReward(r *types.Reward) {
	s.rewards = append(s.rewards, r)
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
		if account.Layer != s.layer {
			continue
		}
		s.log.With().Info("update account",
			s.layer,
			log.Stringer("account", account.Address),
			log.Uint64("nonce", account.Nonce),
			log.Uint64("balance", account.Balance),
		)
		if err := accounts.Update(tx, account); err != nil {
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

	for _, r := range s.rewards {
		if err := rewards.Add(tx, r); err != nil {
			return types.Hash32{}, err
		}
		s.log.With().Info("update rewards",
			s.layer,
			log.Stringer("coinbase", r.Coinbase),
			log.Uint64("total_reward", r.TotalReward),
			log.Uint64("layer_reward", r.LayerReward),
		)
	}

	if err := tx.Commit(); err != nil {
		return types.Hash32{}, err
	}
	return hash, nil
}
