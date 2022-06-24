package niposts

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAddGet(t *testing.T) {
	db := sql.InMemory()
	key := []byte("key1")
	val := types.RandomBytes(411)
	require.NoError(t, Add(db, key, val))

	got, err := Get(db, key)
	require.NoError(t, err)
	require.Equal(t, val, got)

	newVal := types.RandomBytes(311)
	require.NoError(t, Add(db, key, newVal))

	got, err = Get(db, key)
	require.NoError(t, err)
	require.Equal(t, newVal, got)

	_, err = Get(db, []byte("not here"))
	require.ErrorIs(t, err, sql.ErrNotFound)
}
