package dbsync

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

func TestDBItemStore_Empty(t *testing.T) {
	db := populateDB(t, 32, nil)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBSet(db, st, 32, 24)
	ctx := context.Background()
	empty, err := s.Empty(ctx)
	require.NoError(t, err)
	require.True(t, empty)
	seq, err := s.Items(ctx)
	require.NoError(t, err)
	requireEmpty(t, seq)
	for _, _ = range seq {
		require.Fail(t, "expected an empty sequence")
	}

	info, err := s.GetRangeInfo(ctx,
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		-1)
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.String())
	requireEmpty(t, info.Items)

	info, err = s.GetRangeInfo(ctx,
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("9999000000000000000000000000000000000000000000000000000000000000"),
		-1)
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.String())
	requireEmpty(t, info.Items)
}

func TestDBItemStore(t *testing.T) {
	ids := []types.KeyBytes{
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		types.HexToKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
		types.HexToKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000"),
	}
	ctx := context.Background()
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBSet(db, st, 32, 24)
	seq, err := s.Items(ctx)
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		firstKey(t, seq).String())
	has, err := s.Has(ctx, types.HexToKeyBytes("9876000000000000000000000000000000000000000000000000000000000000"))
	require.NoError(t, err)
	require.False(t, has)

	for _, tc := range []struct {
		xIdx, yIdx       int
		limit            int
		fp               string
		count            int
		startIdx, endIdx int
	}{
		{
			xIdx:     0,
			yIdx:     0,
			limit:    0,
			fp:       "000000000000000000000000",
			count:    0,
			startIdx: 0,
			endIdx:   0,
		},
		{
			xIdx:     1,
			yIdx:     1,
			limit:    -1,
			fp:       "642464b773377bbddddddddd",
			count:    5,
			startIdx: 1,
			endIdx:   1,
		},
		{
			xIdx:     0,
			yIdx:     3,
			limit:    -1,
			fp:       "4761032dcfe98ba555555555",
			count:    3,
			startIdx: 0,
			endIdx:   3,
		},
		{
			xIdx:     2,
			yIdx:     0,
			limit:    -1,
			fp:       "761032cfe98ba54ddddddddd",
			count:    3,
			startIdx: 2,
			endIdx:   0,
		},
		{
			xIdx:     3,
			yIdx:     2,
			limit:    3,
			fp:       "2345679abcdef01888888888",
			count:    3,
			startIdx: 3,
			endIdx:   1,
		},
	} {
		name := fmt.Sprintf("%d-%d_%d", tc.xIdx, tc.yIdx, tc.limit)
		t.Run(name, func(t *testing.T) {
			t.Logf("x %s y %s limit %d", ids[tc.xIdx], ids[tc.yIdx], tc.limit)
			info, err := s.GetRangeInfo(ctx, ids[tc.xIdx], ids[tc.yIdx], tc.limit)
			require.NoError(t, err)
			require.Equal(t, tc.count, info.Count)
			require.Equal(t, tc.fp, info.Fingerprint.String())
			require.Equal(t, ids[tc.startIdx], firstKey(t, info.Items))
			has, err := s.Has(ctx, ids[tc.startIdx])
			require.NoError(t, err)
			require.True(t, has)
			has, err = s.Has(ctx, ids[tc.endIdx])
			require.NoError(t, err)
			require.True(t, has)
		})
	}
}

func TestDBItemStore_Add(t *testing.T) {
	ids := []types.KeyBytes{
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		types.HexToKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBSet(db, st, 32, 24)
	ctx := context.Background()
	seq, err := s.Items(ctx)
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		firstKey(t, seq).String())

	newID := types.HexToKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000")
	require.NoError(t, s.Add(context.Background(), newID))

	// // QQQQQ: rm
	// s.ft.traceEnabled = true
	// var sb strings.Builder
	// s.ft.dump(&sb)
	// t.Logf("tree:\n%s", sb.String())

	info, err := s.GetRangeInfo(ctx, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 3, info.Count)
	require.Equal(t, "761032cfe98ba54ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))
}

func TestDBItemStore_Copy(t *testing.T) {
	ids := []types.KeyBytes{
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		types.HexToKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBSet(db, st, 32, 24)
	ctx := context.Background()
	seq, err := s.Items(ctx)
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		firstKey(t, seq).String())

	copy := s.Copy()

	info, err := copy.GetRangeInfo(ctx, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))

	newID := types.HexToKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000")
	require.NoError(t, copy.Add(context.Background(), newID))

	info, err = s.GetRangeInfo(ctx, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ctx, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 3, info.Count)
	require.Equal(t, "761032cfe98ba54ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))
}

func TestDBItemStore_Advance(t *testing.T) {
	ids := []types.KeyBytes{
		types.HexToKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		types.HexToKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		types.HexToKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBSet(db, st, 32, 24)
	ctx := context.Background()
	require.NoError(t, s.EnsureLoaded(ctx))

	copy := s.Copy()

	info, err := s.GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	insertDBItems(t, db, []types.KeyBytes{
		types.HexToKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000"),
	})

	info, err = s.GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	require.NoError(t, s.Advance(ctx))

	info, err = s.GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 5, info.Count)
	require.Equal(t, "642464b773377bbddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = s.Copy().GetRangeInfo(ctx, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 5, info.Count)
	require.Equal(t, "642464b773377bbddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))
}
