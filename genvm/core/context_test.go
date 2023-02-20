package core_test

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/core/mocks"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestTransfer(t *testing.T) {
	t.Run("NoBalance", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(core.DBLoader{sql.InMemory()})}
		require.ErrorIs(t, ctx.Transfer(core.Address{}, 100), core.ErrNoBalance)
	})
	t.Run("MaxSpend", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(core.DBLoader{sql.InMemory()})}
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.MaxSpend = 100
		require.NoError(t, ctx.Transfer(core.Address{1}, 50))
		require.ErrorIs(t, ctx.Transfer(core.Address{2}, 100), core.ErrMaxSpend)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(core.DBLoader{sql.InMemory()})}
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
	t.Run("NoBalance", func(t *testing.T) {
		ctx := core.Context{}
		ctx.Header.GasPrice = 1
		require.ErrorIs(t, ctx.Consume(100), core.ErrNoBalance)
	})
	t.Run("MaxGas", func(t *testing.T) {
		ctx := core.Context{}
		ctx.PrincipalAccount.Balance = 200
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		ctx := core.Context{}
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

func TestApply(t *testing.T) {
	t.Run("UpdatesNonce", func(t *testing.T) {
		ss := core.NewStagedCache(core.DBLoader{sql.InMemory()})
		ctx := core.Context{Loader: ss}
		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.Header.Nonce = 10

		err := ctx.Apply(ss)
		require.NoError(t, err)

		account, err := ss.Get(ctx.PrincipalAccount.Address)
		require.NoError(t, err)
		require.Equal(t, ctx.PrincipalAccount.NextNonce, account.NextNonce)
	})
	t.Run("ConsumeMaxGas", func(t *testing.T) {
		ss := core.NewStagedCache(core.DBLoader{sql.InMemory()})

		ctx := core.Context{Loader: ss}
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10

		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.Header.Nonce = 10

		require.NoError(t, ctx.Consume(5))
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
		err := ctx.Apply(ss)
		require.NoError(t, err)
		require.Equal(t, ctx.Fee(), ctx.Header.MaxGas*ctx.Header.GasPrice)
	})
	t.Run("PreserveTransferOrder", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(core.DBLoader{sql.InMemory()})}
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
		err := ctx.Apply(updater)
		require.NoError(t, err)
		require.Equal(t, order, actual)
	})
}

type empty struct{}

func (e *empty) EncodeScale(*scale.Encoder) (int, error) {
	return 0, nil
}

func (e *empty) DecodeScale(*scale.Decoder) (int, error) {
	return 0, nil
}

func TestRelay(t *testing.T) {
	var (
		template = core.Address{'t', 'e', 'm'}
		remote   = core.Address{'r', 'e', 'm'}
	)
	t.Run("not spawned", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{sql.InMemory()})
		ctx := core.Context{Loader: cache}
		args := &core.ForeignArgs{
			Target: remote,
		}
		require.ErrorIs(t, ctx.Relay(args), core.ErrNotSpawned)
	})
	t.Run("no method", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{sql.InMemory()})
		require.NoError(t, cache.Update(core.Account{
			Address:         remote,
			TemplateAddress: &template,
		}))

		args := &core.ForeignArgs{
			Target: remote,
			Method: 10,
		}

		reg := registry.New()
		ctx := core.Context{Loader: cache, Registry: reg}
		ctrl := gomock.NewController(t)
		handler := mocks.NewMockHandler(ctrl)
		reg.Register(template, handler)
		handler.EXPECT().Load(gomock.Any()).Return(mocks.NewMockTemplate(ctrl), nil).AnyTimes()
		handler.EXPECT().Args(gomock.Any(), args.Method).Return(nil)

		require.ErrorContains(t, ctx.Relay(args), "unknown method")
	})
	t.Run("failed auth", func(t *testing.T) {
		cache := core.NewStagedCache(core.DBLoader{sql.InMemory()})
		require.NoError(t, cache.Update(core.Account{
			Address:         remote,
			TemplateAddress: &template,
		}))

		args := &core.ForeignArgs{
			Target: remote,
			Method: 10,
		}

		reg := registry.New()
		ctx := core.Context{Loader: cache, Registry: reg}
		ctrl := gomock.NewController(t)
		handler := mocks.NewMockHandler(ctrl)
		reg.Register(template, handler)
		tpl := mocks.NewMockTemplate(ctrl)
		handler.EXPECT().Load(gomock.Any()).Return(tpl, nil).AnyTimes()
		handler.EXPECT().Args(gomock.Any(), args.Method).Return(&empty{})
		tpl.EXPECT().Authorize(gomock.Any()).Return(false)

		require.ErrorIs(t, ctx.Relay(args), core.ErrAuth)
	})
}
