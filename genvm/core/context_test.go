package core_test

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/core/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestSpawn(t *testing.T) {
	t.Run("OnlySelf", func(t *testing.T) {
		template := core.Address{1}
		args := core.PublicKey{1}
		ctx := core.Context{}
		require.ErrorIs(t, ctx.Spawn(template, &args), core.ErrSpawn)
	})
	t.Run("UpdateTemplate", func(t *testing.T) {
		template := core.Address{1}
		args := core.PublicKey{1}
		ctx := core.Context{}
		ctx.Header.Principal = core.ComputePrincipal(template, &args)
		require.NoError(t, ctx.Spawn(template, &args))
		require.Equal(t, &template, ctx.Account.Template)
	})
}

func TestTransfer(t *testing.T) {
	t.Run("NoBalance", func(t *testing.T) {
		ctx := core.Context{}
		require.ErrorIs(t, ctx.Transfer(core.Address{}, 100), core.ErrNoBalance)
	})
	t.Run("MaxSpend", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
		ctx.Account.Balance = 1000
		ctx.Header.MaxSpend = 100
		require.NoError(t, ctx.Transfer(core.Address{1}, 50))
		require.ErrorIs(t, ctx.Transfer(core.Address{2}, 100), core.ErrMaxSpend)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
		ctx.Account.Balance = 1000
		ctx.Header.MaxSpend = 1000
		for _, amount := range []uint64{50, 100, 200, 255} {
			before := ctx.Account.Balance
			require.NoError(t, ctx.Transfer(core.Address{uint8(amount)}, amount))
			after := ctx.Account.Balance
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
		ctx.Account.Balance = 200
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		ctx := core.Context{}
		ctx.Account.Balance = 1000
		ctx.Header.GasPrice = 1
		ctx.Header.MaxGas = 1000
		for _, amount := range []uint64{50, 100, 200, 255} {
			before := ctx.Account.Balance
			require.NoError(t, ctx.Consume(amount))
			after := ctx.Account.Balance
			require.Equal(t, amount, before-after)
		}
	})
}

func TestApply(t *testing.T) {
	t.Run("UpdatesNonceAndState", func(t *testing.T) {
		ss := core.NewStagedCache(sql.InMemory())
		ctx := core.Context{Loader: ss}
		ctx.Account.Address = core.Address{1}
		ctx.Header.Nonce = core.Nonce{Counter: 10}

		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		template := mocks.NewMockTemplate(ctrl)
		rst := []byte{1, 1, 1}
		template.EXPECT().EncodeScale(gomock.Any()).DoAndReturn(func(encoder *scale.Encoder) (int, error) {
			return scale.EncodeByteArray(encoder, rst)
		})
		ctx.Template = template
		err := ctx.Apply(ss)
		require.NoError(t, err)

		account, err := ss.Get(ctx.Account.Address)
		require.NoError(t, err)
		require.Equal(t, ctx.Account.Nonce, account.Nonce)
		require.Equal(t, rst, account.State)
	})
	t.Run("ConsumeMaxGas", func(t *testing.T) {
		ss := core.NewStagedCache(sql.InMemory())

		ctx := core.Context{Loader: ss}
		ctx.Account.Balance = 1000
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10

		ctx.Account.Address = core.Address{1}
		ctx.Header.Nonce = core.Nonce{Counter: 10}

		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		template := mocks.NewMockTemplate(ctrl)
		rst := []byte{1, 1, 1}
		template.EXPECT().EncodeScale(gomock.Any()).DoAndReturn(func(encoder *scale.Encoder) (int, error) {
			return scale.EncodeByteArray(encoder, rst)
		})
		ctx.Template = template
		require.NoError(t, ctx.Consume(5))
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
		err := ctx.Apply(ss)
		require.NoError(t, err)
		require.Equal(t, ctx.Fee(), ctx.Header.MaxGas*ctx.Header.GasPrice)
	})
	t.Run("PreserveTransferOrder", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
		ctx.Account.Address = core.Address{1}
		ctx.Account.Balance = 1000
		ctx.Header.MaxSpend = 1000
		order := []core.Address{ctx.Account.Address}
		for _, amount := range []uint64{50, 100, 200, 255} {
			address := core.Address{uint8(amount)}
			require.NoError(t, ctx.Transfer(address, amount))
			order = append(order, address)
		}

		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		template := mocks.NewMockTemplate(ctrl)
		rst := []byte{1, 1, 1}
		template.EXPECT().EncodeScale(gomock.Any()).DoAndReturn(func(encoder *scale.Encoder) (int, error) {
			return scale.EncodeByteArray(encoder, rst)
		})
		ctx.Template = template

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
