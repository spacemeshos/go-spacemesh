package activeset

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestGetNotFound(t *testing.T) {
	const target = 10
	db := localsql.InMemoryTest(t)
	_, _, _, err := Get(db, Tortoise, target)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestUniquePerEpochPerKind(t *testing.T) {
	const target = 10
	db := localsql.InMemoryTest(t)
	for _, kind := range []Kind{Tortoise, Hare} {
		require.NoError(t, Add(db, kind, target, [32]byte{1}, 50, nil))
		require.ErrorIs(t, Add(db, kind, target, [32]byte{2}, 50, nil), sql.ErrObjectExists)
	}
}

func TestGet(t *testing.T) {
	const target = 10
	db := localsql.InMemoryTest(t)

	expectId := [32]byte{1}
	expectWeight := uint64(50)
	expectSet := []types.ATXID{{1}, {2}, {3}}

	require.NoError(t, Add(db, Tortoise, target, expectId, expectWeight, expectSet))
	id, weight, set, err := Get(db, Tortoise, target)
	require.NoError(t, err)
	require.EqualValues(t, expectId, id)
	require.Equal(t, expectWeight, weight)
	require.Equal(t, expectSet, set)
}

func TestLarge(t *testing.T) {
	db := localsql.InMemoryTest(t)
	expect := make([]types.ATXID, 5_000_000)
	require.NoError(t, Add(db, Tortoise, 1, types.Hash32{1}, 10, expect))
	_, _, set, err := Get(db, Tortoise, 1)
	require.NoError(t, err)
	require.Equal(t, expect, set)
}
