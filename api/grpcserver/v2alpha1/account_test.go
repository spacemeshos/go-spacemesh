package v2alpha1

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testAccount struct {
	Address          types.Address
	TemplateAddress  types.Address
	BalanceCurrent   uint64
	BalanceProjected uint64
	CounterCurrent   uint64
	CounterProjected uint64
}

func TestAccountService_List(t *testing.T) {
	db := statesql.InMemoryTest(t)

	ctrl, ctx := gomock.WithContext(context.Background(), t)
	conState := NewMockaccountConState(ctrl)

	accs := make([]testAccount, 100)
	for i := 0; i < 100; i++ {
		addr := types.Address{}
		binary.BigEndian.PutUint64(addr[:], uint64(i))

		template := wallet.TemplateAddress
		if (i % 2) == 0 {
			template = multisig.TemplateAddress
		}

		accs[i] = testAccount{
			Address:          addr,
			TemplateAddress:  template,
			BalanceCurrent:   uint64(1 + i),
			BalanceProjected: uint64(100 + i),
			CounterCurrent:   uint64(1000 + i),
			CounterProjected: uint64(2000 + i),
		}

		require.NoError(t, accounts.Update(db, &types.Account{
			Layer:           1,
			Address:         addr,
			TemplateAddress: &template,
			NextNonce:       accs[i].CounterCurrent,
			Balance:         accs[i].BalanceCurrent,
		}))
	}

	conState.EXPECT().GetProjection(gomock.Any()).DoAndReturn(func(addr types.Address) (uint64, uint64) {
		for _, acc := range accs {
			if acc.Address == addr {
				return acc.CounterProjected, acc.BalanceProjected
			}
		}
		return 0, 0
	}).AnyTimes()

	svc := NewAccountService(db, conState)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewAccountServiceClient(conn)

	t.Run("limit set too high", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		fmt.Println(list)
		require.Len(t, list.Accounts, 25)
	})

	t.Run("all", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{Limit: 100})
		require.NoError(t, err)
		require.Len(t, list.Accounts, len(accs))
	})

	t.Run("address", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{
			Addresses: []string{accs[0].Address.String()},
			Limit:     10,
		})
		require.NoError(t, err)
		require.Len(t, list.Accounts, 1)
		require.Equal(t, accs[0].BalanceProjected, list.Accounts[0].Projected.Balance)
		require.Equal(t, accs[0].CounterProjected, list.Accounts[0].Projected.Counter)
		require.Equal(t, accs[0].BalanceCurrent, list.Accounts[0].Current.Balance)
		require.Equal(t, accs[0].CounterCurrent, list.Accounts[0].Current.Counter)
	})

	t.Run("multiple addresses", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{
			Addresses: []string{accs[0].Address.String(), accs[1].Address.String()},
			Limit:     10,
		})
		require.NoError(t, err)
		require.Len(t, list.Accounts, 2)
		require.Equal(t, accs[0].BalanceProjected, list.Accounts[0].Projected.Balance)
		require.Equal(t, accs[0].CounterProjected, list.Accounts[0].Projected.Counter)
		require.Equal(t, accs[0].BalanceCurrent, list.Accounts[0].Current.Balance)
		require.Equal(t, accs[0].CounterCurrent, list.Accounts[0].Current.Counter)
		require.Equal(t, accs[1].BalanceProjected, list.Accounts[1].Projected.Balance)
		require.Equal(t, accs[1].CounterProjected, list.Accounts[1].Projected.Counter)
		require.Equal(t, accs[1].BalanceCurrent, list.Accounts[1].Current.Balance)
		require.Equal(t, accs[1].CounterCurrent, list.Accounts[1].Current.Counter)
	})

	t.Run("template address", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.AccountRequest{
			Addresses: []string{accs[0].Address.String()},
			Limit:     10,
		})
		require.NoError(t, err)
		require.Len(t, list.Accounts, 1)
		require.Equal(t, accs[0].TemplateAddress.String(), list.Accounts[0].Template)
	})
}
