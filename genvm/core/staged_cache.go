package core

import (
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

// NewStagedCache returns instance of the staged cache.
func NewStagedCache(db sql.Executor) *StagedCache {
	return &StagedCache{db: db, cache: map[Address]stagedAccount{}}
}

// StagedCache is a passthrough cache for accounts state and enforces order for updated accounts.
type StagedCache struct {
	db sql.Executor
	// list of changed accounts. preserving order
	touched []Address
	cache   map[Address]stagedAccount
}

// Get a copy of the Account state for the address.
func (ss *StagedCache) Get(addr Address) (Account, error) {
	sacc, exist := ss.cache[addr]
	if exist {
		return sacc.Account, nil
	}
	account, err := accounts.Latest(ss.db, address.Address(addr))
	if err != nil {
		return Account{}, err
	}
	ss.cache[addr] = stagedAccount{Account: account}
	return account, nil
}

// Update cache with a copy of the account state.
func (ss *StagedCache) Update(account Account) error {
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

// IterateChanged accounts in the order they were updated.
func (ss *StagedCache) IterateChanged(f func(*Account) bool) {
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
