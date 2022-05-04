package accounts

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/require"
)

func genSeq(address types.Address, n int) []*types.Account {
	seq := []*types.Account{}
	for i := 1; i <= n; i++ {
		seq = append(seq, &types.Account{Address: address, Layer: types.NewLayerID(uint32(i)), Balance: uint64(i)})
	}
	return seq
}

func TestUpdate(t *testing.T) {
	address := types.Address{1, 2, 3}
	db := sql.InMemory()
	seq := genSeq(address, 2)
	for _, update := range seq {
		require.NoError(t, Update(db, update))
	}

	latest, err := Latest(db, address)
	require.NoError(t, err)
	require.Equal(t, seq[len(seq)-1], &latest)
}

func TestRevert(t *testing.T) {
	address := types.Address{1, 1}
	seq := genSeq(address, 10)
	db := sql.InMemory()
	for _, update := range seq {
		require.NoError(t, Update(db, update))
	}

	require.NoError(t, Revert(db, seq[3].Layer))
	latest, err := Latest(db, address)
	require.NoError(t, err)
	require.Equal(t, seq[3], &latest)
}
