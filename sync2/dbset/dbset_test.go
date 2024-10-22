package dbset_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/dbset"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

const (
	testKeyLen = 32
	testDepth  = 24
)

func requireEmpty(t *testing.T, sr rangesync.SeqResult) {
	for range sr.Seq {
		require.Fail(t, "expected an empty sequence")
	}
	require.NoError(t, sr.Error())
}

func firstKey(t *testing.T, sr rangesync.SeqResult) rangesync.KeyBytes {
	k, err := sr.First()
	require.NoError(t, err)
	return k
}

func TestDBSet_Empty(t *testing.T) {
	db := sqlstore.PopulateDB(t, testKeyLen, nil)
	st := &sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := dbset.NewDBSet(db, st, testKeyLen, testDepth)
	empty, err := s.Empty()
	require.NoError(t, err)
	require.True(t, empty)
	requireEmpty(t, s.Items())
	requireEmpty(t, s.Received())

	info, err := s.GetRangeInfo(nil, nil)
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.String())
	requireEmpty(t, info.Items)

	info, err = s.GetRangeInfo(
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"))
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.String())
	requireEmpty(t, info.Items)

	info, err = s.GetRangeInfo(
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("9999000000000000000000000000000000000000000000000000000000000000"))
	require.NoError(t, err)
	require.Equal(t, 0, info.Count)
	require.Equal(t, "000000000000000000000000", info.Fingerprint.String())
	requireEmpty(t, info.Items)
}

func TestDBSet(t *testing.T) {
	ids := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		rangesync.MustParseHexKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
		rangesync.MustParseHexKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000"),
	}
	db := sqlstore.PopulateDB(t, testKeyLen, ids)
	st := &sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := dbset.NewDBSet(db, st, testKeyLen, testDepth)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		firstKey(t, s.Items()).String())
	has, err := s.Has(
		rangesync.MustParseHexKeyBytes("9876000000000000000000000000000000000000000000000000000000000000"))
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
			xIdx:     1,
			yIdx:     1,
			limit:    -1,
			fp:       "642464b773377bbddddddddd",
			count:    5,
			startIdx: 1,
			endIdx:   1,
		},
		{
			xIdx:     -1,
			yIdx:     -1,
			limit:    -1,
			fp:       "642464b773377bbddddddddd",
			count:    5,
			startIdx: 0,
			endIdx:   0,
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
			var x, y rangesync.KeyBytes
			if tc.xIdx >= 0 {
				x = ids[tc.xIdx]
				y = ids[tc.yIdx]
			}
			t.Logf("x %v y %v limit %d", x, y, tc.limit)
			var info rangesync.RangeInfo
			if tc.limit < 0 {
				info, err = s.GetRangeInfo(x, y)
				require.NoError(t, err)
			} else {
				sr, err := s.SplitRange(x, y, tc.limit)
				require.NoError(t, err)
				info = sr.Parts[0]
			}
			require.Equal(t, tc.count, info.Count)
			require.Equal(t, tc.fp, info.Fingerprint.String())
			require.Equal(t, ids[tc.startIdx], firstKey(t, info.Items))
			has, err := s.Has(ids[tc.startIdx])
			require.NoError(t, err)
			require.True(t, has)
			has, err = s.Has(ids[tc.endIdx])
			require.NoError(t, err)
			require.True(t, has)
		})
	}
}

func TestDBItemStore_Receive(t *testing.T) {
	ids := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		rangesync.MustParseHexKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := sqlstore.PopulateDB(t, testKeyLen, ids)
	st := &sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := dbset.NewDBSet(db, st, testKeyLen, testDepth)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		firstKey(t, s.Items()).String())

	newID := rangesync.MustParseHexKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000")
	require.NoError(t, s.Receive(newID))

	recvd := s.Received()
	items, err := recvd.FirstN(1)
	require.NoError(t, err)
	require.NoError(t, err)
	require.Equal(t, []rangesync.KeyBytes{newID}, items)

	info, err := s.GetRangeInfo(ids[2], ids[0])
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.String())
}

func TestDBItemStore_Copy(t *testing.T) {
	ids := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		rangesync.MustParseHexKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := sqlstore.PopulateDB(t, testKeyLen, ids)
	st := &sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := dbset.NewDBSet(db, st, testKeyLen, testDepth)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000",
		firstKey(t, s.Items()).String())

	copy := s.Copy(false)

	info, err := copy.GetRangeInfo(ids[2], ids[0])
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))

	newID := rangesync.MustParseHexKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000")
	require.NoError(t, copy.Receive(newID))

	info, err = s.GetRangeInfo(ids[2], ids[0])
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))

	items, err := s.Received().FirstN(100)
	require.NoError(t, err)
	require.Empty(t, items)

	info, err = s.GetRangeInfo(ids[2], ids[0])
	require.NoError(t, err)
	require.Equal(t, 2, info.Count)
	require.Equal(t, "dddddddddddddddddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[2], firstKey(t, info.Items))

	items, err = copy.(*dbset.DBSet).Received().FirstN(100)
	require.NoError(t, err)
	require.Equal(t, []rangesync.KeyBytes{newID}, items)
}

func TestDBItemStore_Advance(t *testing.T) {
	ids := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		rangesync.MustParseHexKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	db := sqlstore.PopulateDB(t, testKeyLen, ids)
	st := &sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := dbset.NewDBSet(db, st, testKeyLen, testDepth)
	require.NoError(t, s.EnsureLoaded())

	copy := s.Copy(false)

	info, err := s.GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	sqlstore.InsertDBItems(t, db, []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000"),
	})

	info, err = s.GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	require.NoError(t, s.Advance())

	info, err = s.GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 5, info.Count)
	require.Equal(t, "642464b773377bbddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = copy.GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 4, info.Count)
	require.Equal(t, "cfe98ba54761032ddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))

	info, err = s.Copy(false).GetRangeInfo(ids[0], ids[0])
	require.NoError(t, err)
	require.Equal(t, 5, info.Count)
	require.Equal(t, "642464b773377bbddddddddd", info.Fingerprint.String())
	require.Equal(t, ids[0], firstKey(t, info.Items))
}

func TestDBSet_Added(t *testing.T) {
	ids := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("123456789abcdef0000000000000000000000000000000000000000000000000"),
		rangesync.MustParseHexKeyBytes("5555555555555555555555555555555555555555555555555555555555555555"),
		rangesync.MustParseHexKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
		rangesync.MustParseHexKeyBytes("abcdef1234567890000000000000000000000000000000000000000000000000"),
	}
	db := sqlstore.PopulateDB(t, testKeyLen, ids)
	st := &sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	s := dbset.NewDBSet(db, st, testKeyLen, testDepth)
	requireEmpty(t, s.Received())

	add := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("3333333333333333333333333333333333333333333333333333333333333333"),
		rangesync.MustParseHexKeyBytes("4444444444444444444444444444444444444444444444444444444444444444"),
	}
	for _, item := range add {
		require.NoError(t, s.Receive(item))
	}

	require.NoError(t, s.EnsureLoaded())

	added, err := s.Received().FirstN(3)
	require.NoError(t, err)
	require.ElementsMatch(t, []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("3333333333333333333333333333333333333333333333333333333333333333"),
		rangesync.MustParseHexKeyBytes("4444444444444444444444444444444444444444444444444444444444444444"),
	}, added)

	added1, err := s.Copy(false).(*dbset.DBSet).Received().FirstN(3)
	require.NoError(t, err)
	require.ElementsMatch(t, added, added1)
}
