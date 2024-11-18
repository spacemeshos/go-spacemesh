package host

import (
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

func getHost(t *testing.T) (*Host, *core.StagedCache) {
	cache := core.NewStagedCache(core.DBLoader{Executor: statesql.InMemoryTest(t)})
	ctx := &core.Context{Loader: cache, Logger: zaptest.NewLogger(t)}
	host, err := NewHost(ctx)
	require.NoError(t, err)
	t.Cleanup(host.Destroy)
	return host, cache
}

func TestNewHost(t *testing.T) {
	host, _ := getHost(t)

	require.Equal(t, "Athena", host.vm.Name())
}

func TestGetBalance(t *testing.T) {
	host, cache := getHost(t)

	account := types.Account{
		Layer:   types.LayerID(15),
		Address: types.Address{1, 2, 3, 4},
		Balance: 100,
	}
	err := cache.Update(account)
	require.NoError(t, err)
	hostCtx := hostContext{
		host: host.host,
	}
	b := hostCtx.GetBalance(athcon.Address(account.Address))
	require.Equal(t, account.Balance, b)
	b = hostCtx.GetBalance(athcon.Address{5, 4, 3, 2})
	require.Equal(t, uint64(0), b)
}

func TestSetGetStorage(t *testing.T) {
	storageKey := athcon.Bytes32{0xc0, 0xff, 0xee}
	storageValue := athcon.Bytes32{0xde, 0xad, 0xbe, 0xef}

	address := types.Address{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	getHostCtx := func(t *testing.T) *hostContext {
		account := types.Account{
			Address: address,
			Balance: 10000,
			Storage: []types.StorageItem{
				{Key: storageKey, Value: storageValue},
			},
		}
		host, cache := getHost(t)
		err := cache.Update(account)
		require.NoError(t, err)

		hostCtx := &hostContext{
			host: host.host,
		}
		return hostCtx
	}
	t.Run("get existing storage value", func(t *testing.T) {
		t.Parallel()
		hostCtx := getHostCtx(t)
		value := hostCtx.GetStorage(athcon.Address(address), storageKey)
		require.Equal(t, storageValue, value)
	})
	t.Run("modify storage value", func(t *testing.T) {
		t.Parallel()
		hostCtx := getHostCtx(t)
		status := hostCtx.SetStorage(athcon.Address(address), storageKey, athcon.Bytes32{9, 8, 7, 6})
		require.Equal(t, athcon.StorageModified, status)

		value := hostCtx.GetStorage(athcon.Address(address), storageKey)
		require.Equal(t, athcon.Bytes32{9, 8, 7, 6}, value)
	})
	t.Run("get for non-existing account", func(t *testing.T) {
		t.Parallel()
		hostCtx := getHostCtx(t)

		value := hostCtx.GetStorage(athcon.Address{1, 2, 3}, storageKey)
		require.Equal(t, athcon.Bytes32{}, value)
	})
	t.Run("get for non-existing key", func(t *testing.T) {
		t.Parallel()
		hostCtx := getHostCtx(t)

		value := hostCtx.GetStorage(athcon.Address(address), athcon.Bytes32{1, 2, 3})
		require.Equal(t, athcon.Bytes32{}, value)
	})
}
