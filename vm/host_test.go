package vm

import (
	"encoding/binary"
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/getbalance"
)

func TestNewHost(t *testing.T) {
	cache := core.NewStagedCache(core.DBLoader{Executor: statesql.InMemoryTest(t)})
	ctx := &core.Context{Loader: cache}
	host, err := NewHost(athenaLibPath(), ctx, cache, cache)
	require.NoError(t, err)
	defer host.Destroy()

	require.Equal(t, "Athena", host.vm.Name())
}

func TestGetBalance(t *testing.T) {
	cache := core.NewStagedCache(core.DBLoader{Executor: statesql.InMemoryTest(t)})
	ctx := &core.Context{Loader: cache}
	host, err := NewHost(athenaLibPath(), ctx, cache, cache)
	require.NoError(t, err)
	defer host.Destroy()

	account := types.Account{
		Layer:   types.LayerID(15),
		Address: types.Address{1, 2, 3, 4},
		Balance: 100,
	}
	err = cache.Update(account)
	require.NoError(t, err)

	out, gasLeft, err := host.Execute(
		account.Layer,
		10000,
		account.Address,
		account.Address,
		nil,
		0,
		getbalance.PROGRAM,
	)

	require.NoError(t, err)
	balance := binary.LittleEndian.Uint64(out)
	require.Equal(t, account.Balance, balance)
	require.NotZero(t, gasLeft)
}

func TestNotEnoughGas(t *testing.T) {
	cache := core.NewStagedCache(core.DBLoader{Executor: statesql.InMemoryTest(t)})
	ctx := &core.Context{Loader: cache}
	host, err := NewHost(athenaLibPath(), ctx, cache, cache)
	require.NoError(t, err)
	defer host.Destroy()

	_, gasLeft, err := host.Execute(
		10,
		10,
		types.Address{1, 2, 3, 4},
		types.Address{1, 2, 3, 4},
		nil,
		0,
		getbalance.PROGRAM,
	)

	// FIXME: export more errors in athcon
	// 3 is "out of gas"
	require.ErrorIs(t, err, athcon.Error(3))
	require.Zero(t, gasLeft)
}
