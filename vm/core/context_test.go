package core_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/core/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/registry"
)

func TestTransfer(t *testing.T) {
	var principal types.Address
	t.Run("NoBalance", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		require.ErrorIs(t, ctx.Transfer(principal, 100), core.ErrNoBalance)
	})
	t.Run("MaxSpend", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.MaxSpend = 100
		require.NoError(t, ctx.Transfer(core.Address{1}, 50))
		require.ErrorIs(t, ctx.Transfer(core.Address{2}, 100), core.ErrMaxSpend)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.MaxSpend = 1000
		for _, amount := range []uint64{50, 100, 200, 255} {
			before := ctx.PrincipalAccount.Balance
			require.NoError(t, ctx.Transfer(core.Address{uint8(amount)}, amount))
			after := ctx.PrincipalAccount.Balance
			require.Equal(t, amount, before-after)
		}
	})
}

func TestConsume(t *testing.T) {
	var principal types.Address
	t.Run("OutOfGas", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.Header.GasPrice = 1
		require.ErrorIs(t, ctx.Consume(100), core.ErrOutOfGas)
	})
	t.Run("MaxGas", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.PrincipalAccount.Balance = 200
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.GasPrice = 1
		ctx.Header.MaxGas = 1000
		for _, amount := range []uint64{50, 100, 200, 255} {
			before := ctx.PrincipalAccount.Balance
			require.NoError(t, ctx.Consume(amount))
			after := ctx.PrincipalAccount.Balance
			require.Equal(t, amount, before-after)
		}
	})
}

func TestRefund(t *testing.T) {
	account := types.Account{
		Address: types.RandomAddress(t),
		Balance: 100,
	}
	t.Run("empty refund", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		cache.Update(account)
		ctx, err := core.New(types.Hash20{}, 0, account.Address, cache, registry.New(), zaptest.NewLogger(t))

		require.NoError(t, err)
		ctx.Header.GasPrice = 1

		ctx.Refund()
		require.EqualValues(t, 0, ctx.Consumed())
		require.EqualValues(t, 0, ctx.Fee())
		ctx.Apply(cache)
		updatedA, err := cache.Get(account.Address)
		require.NoError(t, err)
		require.Equal(t, account.Balance, updatedA.Balance)
	})
	t.Run("nothing spent - refund all", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		cache.Update(account)
		ctx, err := core.New(types.Hash20{}, 0, account.Address, cache, registry.New(), zaptest.NewLogger(t))

		require.NoError(t, err)
		ctx.Header.MaxGas = 100
		ctx.Header.GasPrice = 1
		require.NoError(t, ctx.Consume(60))
		require.EqualValues(t, 60, ctx.Consumed())

		ctx.Refund()
		require.EqualValues(t, 0, ctx.Consumed())
		require.EqualValues(t, 0, ctx.Fee())
		ctx.Apply(cache)
		updatedA, err := cache.Get(account.Address)
		require.NoError(t, err)
		require.Equal(t, account.Balance, updatedA.Balance)
	})
	t.Run("spent some - refund remaining", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		cache.Update(account)
		ctx, err := core.New(types.Hash20{}, 0, account.Address, cache, registry.New(), zaptest.NewLogger(t))

		require.NoError(t, err)
		ctx.Header.MaxGas = 100
		ctx.Header.GasPrice = 1
		require.NoError(t, ctx.Consume(60))
		require.EqualValues(t, 60, ctx.Consumed())
		ctx.SpendGas(20)
		ctx.Refund()
		require.EqualValues(t, 20, ctx.Consumed())
		require.EqualValues(t, 20, ctx.Fee())
		ctx.Apply(cache)
		updatedA, err := cache.Get(account.Address)
		require.NoError(t, err)
		require.Equal(t, account.Balance-20, updatedA.Balance)
	})
	t.Run("spent over consumed - no refund", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		cache.Update(account)
		ctx, err := core.New(types.Hash20{}, 0, account.Address, cache, registry.New(), zaptest.NewLogger(t))

		require.NoError(t, err)
		ctx.Header.MaxGas = 100
		ctx.Header.GasPrice = 1
		require.NoError(t, ctx.Consume(60))
		require.EqualValues(t, 60, ctx.Consumed())
		ctx.SpendGas(200)
		ctx.Refund()
		require.EqualValues(t, 60, ctx.Consumed())
		require.EqualValues(t, 60, ctx.Fee())
		ctx.Apply(cache)
		updatedA, err := cache.Get(account.Address)
		require.NoError(t, err)
		require.Equal(t, account.Balance-60, updatedA.Balance)
	})
}

func TestApply(t *testing.T) {
	var principal types.Address
	t.Run("UpdatesNonce", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.Header.Nonce = 10

		err = ctx.Apply(cache)
		require.NoError(t, err)

		account, err := cache.Get(ctx.PrincipalAccount.Address)
		require.NoError(t, err)
		require.Equal(t, ctx.PrincipalAccount.NextNonce, account.NextNonce)
	})
	t.Run("ConsumeMaxGas", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)

		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10

		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.Header.Nonce = 10

		require.NoError(t, ctx.Consume(5))
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
		err = ctx.Apply(cache)
		require.NoError(t, err)
		require.Equal(t, ctx.Fee(), ctx.Header.MaxGas*ctx.Header.GasPrice)
	})
	t.Run("PreserveTransferOrder", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)
		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.MaxSpend = 1000
		order := []core.Address{ctx.PrincipalAccount.Address}
		for _, amount := range []uint64{50, 100, 200, 255} {
			address := core.Address{uint8(amount)}
			require.NoError(t, ctx.Transfer(address, amount))
			order = append(order, address)
		}

		ctrl := gomock.NewController(t)

		updater := mocks.NewMockAccountUpdater(ctrl)
		actual := []core.Address{}
		updater.EXPECT().Update(gomock.Any()).Do(func(account core.Account) error {
			actual = append(actual, account.Address)
			return nil
		}).AnyTimes()
		err = ctx.Apply(updater)
		require.NoError(t, err)
		require.Equal(t, order, actual)
	})
}

func TestDeploy(t *testing.T) {
	var principal types.Address
	code := []byte("some bad code")
	templateAddress := core.TemplateAddress(code)
	t.Run("deploying new contract", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)

		addr, err := ctx.Deploy(code)
		require.NoError(t, err)
		require.Equal(t, templateAddress, addr)

		err = ctx.Apply(cache)
		require.NoError(t, err)

		account, err := cache.Get(addr)
		require.NoError(t, err)
		require.Equal(t, code, account.State)
	})
	t.Run("can't deploy twice", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{statesql.InMemoryTest(t)})
		ctx, err := core.New(types.Hash20{}, 0, principal, cache, registry.New(), zaptest.NewLogger(t))
		require.NoError(t, err)

		_, err = ctx.Deploy(code)
		require.NoError(t, err)

		_, err = ctx.Deploy(code)
		require.ErrorContains(t, err, "already deployed")
	})
}
