package activesets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestActiveSet(t *testing.T) {
	ids := []types.Hash32{{1}, {2}, {3}, {4}}
	set := &types.EpochActiveSet{
		Epoch: 2,
		Set:   []types.ATXID{{1}, {2}},
	}
	db := sql.InMemory()

	require.NoError(t, Add(db, ids[0], set))
	require.ErrorIs(t, Add(db, ids[0], set), sql.ErrObjectExists)
	require.NoError(t, Add(db, ids[1], &types.EpochActiveSet{}))

	set1, err := Get(db, ids[0])
	require.NoError(t, err)
	require.Equal(t, set, set1)

	blob, err := GetBlob(db, ids[0].Bytes())
	require.NoError(t, err)
	require.Equal(t, codec.MustEncode(set), blob)

	set2, err := Get(db, ids[1])
	require.NoError(t, err)
	require.Empty(t, set2)

	_, err = Get(db, ids[3])
	require.ErrorIs(t, err, sql.ErrNotFound)
	_, err = GetBlob(db, ids[3].Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, DeleteBeforeEpoch(db, set.Epoch))
	blob, err = GetBlob(db, ids[0].Bytes())
	require.NoError(t, err)
	require.NotEmpty(t, blob)

	require.NoError(t, DeleteBeforeEpoch(db, set.Epoch+1))
	blob, err = GetBlob(db, ids[0].Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, blob)
}
