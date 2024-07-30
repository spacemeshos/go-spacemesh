package core

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

type DBLoader struct {
	sql.Executor
}

func (db DBLoader) Get(address types.Address) (types.Account, error) {
	return accounts.Latest(db.Executor, address)
}

// NewStagedCache returns instance of the staged cache.
func NewStagedCache(loader AccountLoader) *StagedCache {
	return &StagedCache{loader: loader, cache: map[Address]stagedAccount{}}
}

// StagedCache is a passthrough cache for accounts state and enforces order for updated accounts.
type StagedCache struct {
	loader AccountLoader
	// list of changed accounts. preserving order
	touched []Address
	cache   map[Address]stagedAccount
}

// Get a copy of the Account state for the address.
func (ss *StagedCache) Get(address Address) (Account, error) {
	sacc, exist := ss.cache[address]
	if exist {
		return sacc.Account, nil
	}
	account, err := ss.loader.Get(address)
	if err != nil {
		return Account{}, err
	}
	ss.cache[address] = stagedAccount{Account: account}
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
