package vm

import (
	"encoding/binary"
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/programs/getbalance"
	hostprogram "github.com/spacemeshos/go-spacemesh/vm/programs/host"
)

func getHost(t *testing.T) (*Host, *core.StagedCache) {
	cache := core.NewStagedCache(core.DBLoader{Executor: statesql.InMemoryTest(t)})
	ctx := &core.Context{Loader: cache}
	staticContext := StaticContext{
		principal:   types.Address{1, 2, 3, 4},
		destination: types.Address{5, 6, 7, 8},
		nonce:       10,
	}
	dynamicContext := DynamicContext{
		template: types.Address{11, 12, 13, 14},
		callee:   types.Address{15, 16, 17, 18},
	}
	host, err := NewHost(ctx, cache, cache, staticContext, dynamicContext)
	require.NoError(t, err)
	return host, cache
}

func TestNewHost(t *testing.T) {
	host, _ := getHost(t)
	defer host.Destroy()

	require.Equal(t, "Athena", host.vm.Name())
}

func TestGetBalance(t *testing.T) {
	host, cache := getHost(t)
	defer host.Destroy()

	account := types.Account{
		Layer:   types.LayerID(15),
		Address: types.Address{1, 2, 3, 4},
		Balance: 100,
	}
	err := cache.Update(account)
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
	host, _ := getHost(t)
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

	require.ErrorIs(t, err, athcon.OutOfGas)
	require.Zero(t, gasLeft)
}

func TestEmptyCode(t *testing.T) {
	host, _ := getHost(t)
	defer host.Destroy()

	_, _, err := host.Execute(
		10,
		10,
		types.Address{1, 2, 3, 4},
		types.Address{1, 2, 3, 4},
		nil,
		0,
		[]byte{},
	)

	require.Equal(t, athcon.Failure, err)
}

func TestSetGetStorge(t *testing.T) {
	host, cache := getHost(t)
	defer host.Destroy()

	storageKey := athcon.Bytes32{0xc0, 0xff, 0xee}
	storageValue := athcon.Bytes32{0xde, 0xad, 0xbe, 0xef}

	address := types.Address{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	account := types.Account{
		Address: address,
		Balance: 1000000,
		Storage: []types.StorageItem{
			{Key: storageKey, Value: storageValue},
		},
	}
	err := cache.Update(account)
	require.NoError(t, err)

	_, gasLeft, err := host.Execute(
		account.Layer,
		100000,
		account.Address,
		account.Address,
		nil,
		0,
		hostprogram.PROGRAM,
	)

	require.NoError(t, err)
	require.NotZero(t, gasLeft)
}
