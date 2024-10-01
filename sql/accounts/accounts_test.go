package accounts

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func genSeq(address types.Address, n int) []*types.Account {
	seq := []*types.Account{}
	for i := 1; i <= n; i++ {
		seq = append(seq, &types.Account{Address: address, Layer: types.LayerID(uint32(i)), Balance: uint64(i)})
	}
	return seq
}

func TestUpdate(t *testing.T) {
	address := types.Address{1, 2, 3}
	db := statesql.InMemory()
	seq := genSeq(address, 2)
	for _, update := range seq {
		require.NoError(t, Update(db, update))
	}

	latest, err := Latest(db, address)
	require.NoError(t, err)
	require.Equal(t, seq[len(seq)-1], &latest)
}

func TestHas(t *testing.T) {
	address := types.Address{1, 2, 3}
	db := statesql.InMemory()
	has, err := Has(db, address)
	require.NoError(t, err)
	require.False(t, has)
	seq := genSeq(address, 2)
	for _, update := range seq {
		require.NoError(t, Update(db, update))
	}
	has, err = Has(db, address)
	require.NoError(t, err)
	require.True(t, has)
}

func TestRevert(t *testing.T) {
	address := types.Address{1, 1}
	seq := genSeq(address, 10)
	db := statesql.InMemory()
	for _, update := range seq {
		require.NoError(t, Update(db, update))
	}

	require.NoError(t, Revert(db, seq[3].Layer))
	latest, err := Latest(db, address)
	require.NoError(t, err)
	require.Equal(t, seq[3], &latest)
}

func TestAll(t *testing.T) {
	db := statesql.InMemory()
	addresses := []types.Address{{1, 1}, {2, 2}, {3, 3}}
	n := []int{10, 7, 20}
	for i, address := range addresses {
		for _, update := range genSeq(address, n[i]) {
			require.NoError(t, Update(db, update))
		}
	}

	accounts, err := All(db)
	require.NoError(t, err)
	require.Len(t, accounts, len(addresses))
	for i, address := range addresses {
		require.Equal(t, address, accounts[i].Address)
		require.EqualValues(t, n[i], accounts[i].Layer)
	}
}

func TestSnapshot(t *testing.T) {
	db := statesql.InMemory()

	_, err := Snapshot(db, types.LayerID(1))
	require.ErrorIs(t, err, sql.ErrNotFound)

	addresses := []types.Address{{1, 1}, {2, 2}, {3, 3}}
	n := []int{10, 7, 20}
	for i, address := range addresses {
		for _, update := range genSeq(address, n[i]) {
			require.NoError(t, Update(db, update))
		}
	}
	for lid := types.LayerID(20); lid.After(0); lid-- {
		got, err := Snapshot(db, lid)
		require.NoError(t, err)
		for i, address := range addresses {
			require.Equal(t, address, got[i].Address)
			if uint32(n[i]) > lid.Uint32() {
				require.EqualValues(t, lid, got[i].Layer)
			} else {
				require.EqualValues(t, n[i], got[i].Layer)
			}
		}
	}
}

func TestIterateAccountsOps(t *testing.T) {
	db := statesql.InMemory()

	for i := 0; i < 100; i++ {
		addr := types.Address{}
		binary.BigEndian.PutUint64(addr[:], uint64(i))

		require.NoError(t, Update(db, &types.Account{
			Layer:     1,
			Address:   addr,
			NextNonce: uint64(1000 + i),
			Balance:   uint64(1 + i),
		}))
	}
	ops := builder.Operations{}
	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.GroupBy,
		Value: "address",
	})

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "layer_updated desc",
	})

	t.Run("callback return true", func(t *testing.T) {
		var accs []*types.Account
		err := IterateAccountsOps(db, ops, func(account *types.Account) bool {
			accs = append(accs, account)
			return true
		})
		require.NoError(t, err)
		require.Len(t, accs, 100)
	})

	t.Run("callback return false", func(t *testing.T) {
		var accs []*types.Account
		err := IterateAccountsOps(db, ops, func(account *types.Account) bool {
			accs = append(accs, account)
			return false
		})
		require.NoError(t, err)
		require.Len(t, accs, 1)
	})
}

func TestStorage(t *testing.T) {
	db := statesql.InMemory()
	address := types.Address{1, 2, 3}
	templateAddress := types.Address{4, 5, 6}
	storage := types.StorageItems{
		{Key: types.Hash32{1, 2, 3}, Value: types.Hash32{4, 5, 6}},
		{Key: types.Hash32{4, 5, 6}, Value: types.Hash32{7, 8, 9}},
	}
	require.NoError(t, Update(db, &types.Account{
		Address:         address,
		TemplateAddress: &templateAddress,
		Storage:         storage,
	}))

	latest, err := Latest(db, address)
	require.NoError(t, err)
	require.Equal(t, storage, latest.Storage)
}
