package activesets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestActiveSet(t *testing.T) {
	ctx := context.Background()

	ids := []types.Hash32{{1}, {2}, {3}, {4}}
	set := &types.EpochActiveSet{
		Epoch: 2,
		Set:   []types.ATXID{{1}, {2}},
	}
	db := statesql.InMemory()

	require.NoError(t, Add(db, ids[0], set))
	require.ErrorIs(t, Add(db, ids[0], set), sql.ErrObjectExists)
	require.NoError(t, Add(db, ids[1], &types.EpochActiveSet{}))

	set1, err := Get(db, ids[0])
	require.NoError(t, err)
	require.Equal(t, set, set1)

	var blob1 sql.Blob
	require.NoError(t, LoadBlob(ctx, db, ids[0].Bytes(), &blob1))
	require.Equal(t, codec.MustEncode(set), blob1.Bytes)

	set2, err := Get(db, ids[1])
	require.NoError(t, err)
	require.Empty(t, set2)

	var blob2 sql.Blob
	require.NoError(t, LoadBlob(ctx, db, ids[1].Bytes(), &blob2))
	require.Equal(t, codec.MustEncode(set2), blob2.Bytes)

	_, err = Get(db, ids[3])
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.ErrorIs(t, LoadBlob(ctx, db, ids[3].Bytes(), &sql.Blob{}), sql.ErrNotFound)

	sizes, err := GetBlobSizes(db, [][]byte{ids[0].Bytes(), ids[1].Bytes(), ids[3].Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes), -1}, sizes)

	require.NoError(t, DeleteBeforeEpoch(db, set.Epoch))
	require.NoError(t, LoadBlob(ctx, db, ids[0].Bytes(), &blob1))
	require.NotEmpty(t, blob1.Bytes)

	require.NoError(t, DeleteBeforeEpoch(db, set.Epoch+1))
	require.ErrorIs(t, LoadBlob(ctx, db, ids[0].Bytes(), &sql.Blob{}), sql.ErrNotFound)
}

func TestCachedActiveSet(t *testing.T) {
	ctx := context.Background()
	ids := []types.Hash32{{1}, {2}}
	set0 := &types.EpochActiveSet{
		Epoch: 2,
		Set:   []types.ATXID{{1}, {2}},
	}
	set1 := &types.EpochActiveSet{
		Epoch: 2,
		Set:   []types.ATXID{{3}, {4}},
	}
	db := statesql.InMemory(sql.WithQueryCache(true))

	require.NoError(t, Add(db, ids[0], set0))
	require.NoError(t, Add(db, ids[1], set1))
	require.Equal(t, 2, db.QueryCount())

	var b sql.Blob
	for i := 0; i < 3; i++ {
		require.NoError(t, LoadBlob(ctx, db, ids[0].Bytes(), &b))
		require.Equal(t, codec.MustEncode(set0), b.Bytes)
		require.Equal(t, 3, db.QueryCount(), "ids[0]: QueryCount at i=%d", i)
	}

	for i := 0; i < 3; i++ {
		require.NoError(t, LoadBlob(ctx, db, ids[1].Bytes(), &b))
		require.Equal(t, codec.MustEncode(set1), b.Bytes)
		require.Equal(t, 4, db.QueryCount(), "ids[1]: QueryCount at i=%d", i)
	}
}
