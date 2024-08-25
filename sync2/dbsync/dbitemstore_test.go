package dbsync

import (
	"context"
	"fmt"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/stretchr/testify/require"
)

func TestDBItemStore_Empty(t *testing.T) {
	db := populateDB(t, 32, nil)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBItemStore(db, st, 32, 24)
	ctx := context.Background()
	it, err := s.Min(ctx)
	require.NoError(t, err)
	require.Nil(t, it)

	info, err := s.GetRangeInfo(ctx, nil,
		KeyBytes(util.FromHex("0000000000000000000000000000000000000000000000000000000000000000")),
		KeyBytes(util.FromHex("0000000000000000000000000000000000000000000000000000000000000000")),
		-1)
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.(fmt.Stringer).String())
	require.Nil(t, info.Start)
	require.Nil(t, info.End)

	info, err = s.GetRangeInfo(ctx, nil,
		KeyBytes(util.FromHex("0000000000000000000000000000000000000000000000000000000000000000")),
		KeyBytes(util.FromHex("9999000000000000000000000000000000000000000000000000000000000000")),
		-1)
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.(fmt.Stringer).String())
	require.Nil(t, info.Start)
	require.Nil(t, info.End)
}

func TestDBItemStore(t *testing.T) {
	ids := []KeyBytes{
		util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
		util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
		util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
		util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
		util.FromHex("abcdef1234567890000000000000000000000000000000000000000000000000"),
	}
	ctx := context.Background()
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBItemStore(db, st, 32, 24)
	it, err := s.Min(ctx)
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		itKey(t, it).String())
	has, err := s.Has(ctx, KeyBytes(util.FromHex("9876000000000000000000000000000000000000000000000000000000000000")))
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
			info, err := s.GetRangeInfo(ctx, nil, ids[tc.xIdx], ids[tc.yIdx], tc.limit)
			require.NoError(t, err)
			require.Equal(t, tc.count, info.Count)
			require.Equal(t, tc.fp, info.Fingerprint.(fmt.Stringer).String())
			require.Equal(t, ids[tc.startIdx], itKey(t, info.Start))
			require.Equal(t, ids[tc.endIdx], itKey(t, info.End))
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
	ids := []KeyBytes{
		util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
		util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
		util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
		util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBItemStore(db, st, 32, 24)
	ctx := context.Background()
	it, err := s.Min(ctx)
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		itKey(t, it).String())

	newID := KeyBytes(util.FromHex("abcdef1234567890000000000000000000000000000000000000000000000000"))
	require.NoError(t, s.Add(context.Background(), newID))

	// // QQQQQ: rm
	// s.ft.traceEnabled = true
	// var sb strings.Builder
	// s.ft.dump(&sb)
	// t.Logf("tree:\n%s", sb.String())

	info, err := s.GetRangeInfo(ctx, nil, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 3, info.Count)
	require.Equal(t, "761032cfe98ba54ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[2], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))
}

func TestDBItemStore_Copy(t *testing.T) {
	ids := []KeyBytes{
		util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
		util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
		util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
		util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBItemStore(db, st, 32, 24)
	ctx := context.Background()
	it, err := s.Min(ctx)
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		itKey(t, it).String())

	copy := s.Copy()

	info, err := copy.GetRangeInfo(ctx, nil, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[2], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	newID := KeyBytes(util.FromHex("abcdef1234567890000000000000000000000000000000000000000000000000"))
	require.NoError(t, copy.Add(context.Background(), newID))

	info, err = s.GetRangeInfo(ctx, nil, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[2], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	info, err = copy.GetRangeInfo(ctx, nil, ids[2], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 3, info.Count)
	require.Equal(t, "761032cfe98ba54ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[2], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))
}

func TestDBItemStore_Advance(t *testing.T) {
	ids := []KeyBytes{
		util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
		util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
		util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
		util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := populateDB(t, 32, ids)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := NewDBItemStore(db, st, 32, 24)
	ctx := context.Background()
	require.NoError(t, s.EnsureLoaded(ctx))

	copy := s.Copy()

	info, err := s.GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	info, err = copy.GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	insertDBItems(t, db, []KeyBytes{
		util.FromHex("abcdef1234567890000000000000000000000000000000000000000000000000"),
	})

	info, err = s.GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	info, err = copy.GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	require.NoError(t, s.Advance(ctx))

	info, err = s.GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 5, info.Count)
	require.Equal(t, "642464b773377bbddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	info, err = copy.GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))

	info, err = s.Copy().GetRangeInfo(ctx, nil, ids[0], ids[0], -1)
	require.NoError(t, err)
	require.Equal(t, 5, info.Count)
	require.Equal(t, "642464b773377bbddddddddd", info.Fingerprint.(fmt.Stringer).String())
	require.Equal(t, ids[0], itKey(t, info.Start))
	require.Equal(t, ids[0], itKey(t, info.End))
}
