package core

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

func NewStagedState(db sql.Executor) *StagedState {
	return &StagedState{db: db, cache: map[Address]stagedAccount{}}
}

type StagedState struct {
	db sql.Executor
	// list of changed accounts, preserving order
	touched []Address
	cache   map[Address]stagedAccount
}

func (ss *StagedState) Get(address Address) (Account, error) {
	sacc, exist := ss.cache[address]
	if exist {
		return sacc.Account, nil
	}
	account, err := accounts.Latest(ss.db, types.Address(address))
	if err != nil {
		return Account{}, err
	}
	ss.cache[address] = stagedAccount{Account: account}
	return account, nil
}

func (ss *StagedState) Update(account Account) error {
	sacc, exist := ss.cache[Address(account.Address)]
	if !exist || !sacc.Changed {
		ss.touched = append(ss.touched, Address(account.Address))
	}
	ss.cache[Address(account.Address)] = stagedAccount{
		Account: account,
		Changed: true,
	}
	return nil
}

func (ss *StagedState) IterateChanged(f func(*Account) bool) {
	for _, address := range ss.touched {
		account := ss.cache[address]
		if !f(&account.Account) {
			return
		}
	}
}

type stagedAccount struct {
	Account
	Changed bool
}
