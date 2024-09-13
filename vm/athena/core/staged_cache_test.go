package core_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/athena/core"
)

func TestCacheGetCopies(t *testing.T) {
	db := statesql.InMemory()
	ss := core.NewStagedCache(core.DBLoader{db})
	address := core.Address{1}
	account, err := ss.Get(address)
	require.NoError(t, err)
	account.Balance = 100

	accountCopy, err := ss.Get(address)
	require.NoError(t, err)
	require.Empty(t, accountCopy.Balance)
}

func TestCacheUpdatePreserveOrder(t *testing.T) {
	db := statesql.InMemory()
	ss := core.NewStagedCache(core.DBLoader{db})
	order := []core.Address{{3}, {1}, {2}}
	for _, address := range order {
		require.NoError(t, ss.Update(core.Account{Address: address}))
	}
	actual := []core.Address{}
	ss.IterateChanged(func(account *core.Account) bool {
		actual = append(actual, account.Address)
		return true
	})
	require.Equal(t, order, actual)
}
