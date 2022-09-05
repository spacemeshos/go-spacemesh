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
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
		require.ErrorIs(t, ctx.Transfer(core.Address{}, 100), core.ErrNoBalance)
	})
	t.Run("MaxSpend", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.MaxSpend = 100
		require.NoError(t, ctx.Transfer(core.Address{1}, 50))
		require.ErrorIs(t, ctx.Transfer(core.Address{2}, 100), core.ErrMaxSpend)
	})
	t.Run("ReducesBalance", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
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
		ss := core.NewStagedCache(sql.InMemory())
		ctx := core.Context{Loader: ss}
		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.Header.Nonce = core.Nonce{Counter: 10}

		err := ctx.Apply(ss)
		require.NoError(t, err)

		account, err := ss.Get(ctx.PrincipalAccount.Address)
		require.NoError(t, err)
		require.Equal(t, ctx.PrincipalAccount.NextNonce, account.NextNonce)
	})
	t.Run("ConsumeMaxGas", func(t *testing.T) {
		ss := core.NewStagedCache(sql.InMemory())

		ctx := core.Context{Loader: ss}
		ctx.PrincipalAccount.Balance = 1000
		ctx.Header.GasPrice = 2
		ctx.Header.MaxGas = 10

		ctx.PrincipalAccount.Address = core.Address{1}
		ctx.Header.Nonce = core.Nonce{Counter: 10}

		require.NoError(t, ctx.Consume(5))
		require.ErrorIs(t, ctx.Consume(100), core.ErrMaxGas)
		err := ctx.Apply(ss)
		require.NoError(t, err)
		require.Equal(t, ctx.Fee(), ctx.Header.MaxGas*ctx.Header.GasPrice)
	})
	t.Run("PreserveTransferOrder", func(t *testing.T) {
		ctx := core.Context{Loader: core.NewStagedCache(sql.InMemory())}
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
		defer ctrl.Finish()

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

func TestRelay(t *testing.T) {
	var (
		principal = core.Address{'p', 'r', 'i'}
		template  = core.Address{'t', 'e', 'm'}
		remote    = core.Address{'r', 'e', 'm'}
	)
	t.Run("not spawned", func(t *testing.T) {
		cache := core.NewStagedCache(sql.InMemory())
		ctx := core.Context{Loader: cache}
		call := func(remote core.Host) error {
			require.Fail(t, "not expected to be called")
			return nil
		}
		require.ErrorIs(t, ctx.Relay(template, remote, call), core.ErrNotSpawned)
	})
	t.Run("mismatched template", func(t *testing.T) {
		cache := core.NewStagedCache(sql.InMemory())
		require.NoError(t, cache.Update(core.Account{
			Address:  remote,
			Template: &core.Address{'m', 'i', 's'},
		}))
		ctx := core.Context{Loader: cache}
		call := func(remote core.Host) error {
			require.Fail(t, "not expected to be called")
			return nil
		}
		require.ErrorIs(t, ctx.Relay(template, remote, call), core.ErrTemplateMismatch)
	})
	t.Run("relayed transfer", func(t *testing.T) {
		for _, receiver1 := range []core.Address{{'a', 'n', 'y'}, principal} {
			t.Run(string(receiver1[:3]), func(t *testing.T) {
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()

				encoded := []byte("test")

				handler := mocks.NewMockHandler(ctrl)
				tpl := mocks.NewMockTemplate(ctrl)
				tpl.EXPECT().EncodeScale(gomock.Any()).DoAndReturn(func(enc *scale.Encoder) (int, error) {
					return scale.EncodeByteArray(enc, encoded)
				}).AnyTimes()
				handler.EXPECT().Load(gomock.Any()).Return(tpl, nil).AnyTimes()
				reg := registry.New()
				reg.Register(template, handler)

				cache := core.NewStagedCache(sql.InMemory())
				receiver2 := core.Address{'f'}
				const (
					total   = 1000
					amount1 = 100
					amount2 = total - amount1/2
				)
				require.NoError(t, cache.Update(core.Account{
					Address:  remote,
					Template: &template,
					Balance:  total,
				}))
				ctx := core.Context{Loader: cache, Registry: reg}
				ctx.PrincipalAccount.Address = principal
				require.NoError(t, ctx.Relay(template, remote, func(remote core.Host) error {
					return remote.Transfer(receiver1, amount1)
				}))
				require.ErrorIs(t, ctx.Relay(template, remote, func(remote core.Host) error {
					return remote.Transfer(receiver2, amount2)
				}), core.ErrNoBalance)
				require.NoError(t, ctx.Apply(cache))

				rec1state, err := cache.Get(receiver1)
				require.NoError(t, err)
				require.Equal(t, amount1, int(rec1state.Balance))
				require.NotEqual(t, encoded, rec1state.State)

				rec2state, err := cache.Get(receiver2)
				require.NoError(t, err)
				require.Equal(t, 0, int(rec2state.Balance))
				require.NotEqual(t, encoded, rec2state.State)

				remoteState, err := cache.Get(remote)
				require.NoError(t, err)
				require.Equal(t, total-amount1, int(remoteState.Balance))
				require.Equal(t, encoded, remoteState.State)
			})
		}
	})
}
