package sqlstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/expr"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

func TestSyncedTable_GenSQL(t *testing.T) {
	for _, tc := range []struct {
		name        string
		st          sqlstore.SyncedTable
		all         string
		count       string
		maxRowID    string
		idRange     string
		recent      string
		recentCount string
	}{
		{
			name: "no filter",
			st: sqlstore.SyncedTable{
				TableName:       "atxs",
				IDColumn:        "id",
				TimestampColumn: "received",
			},
			all:      `SELECT "id" FROM "atxs" WHERE "rowid" <= ?`,
			count:    `SELECT count("id") FROM "atxs" WHERE "rowid" <= ?`,
			maxRowID: `SELECT max("rowid") FROM "atxs"`,
			idRange: `SELECT "id" FROM "atxs" WHERE "id" >= ? AND "rowid" <= ? ` +
				`ORDER BY "id" LIMIT ?`,
			recent: `SELECT "id" FROM "atxs" WHERE "id" >= ? AND "rowid" <= ? ` +
				`AND "received" >= ? ORDER BY "id" LIMIT ?`,
			recentCount: `SELECT count("id") FROM "atxs" WHERE "rowid" <= ? ` +
				`AND "received" >= ?`,
		},
		{
			name: "filter",
			st: sqlstore.SyncedTable{
				TableName:       "atxs",
				IDColumn:        "id",
				Filter:          expr.MustParse("epoch = ?"),
				TimestampColumn: "received",
			},
			all:      `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "rowid" <= ?`,
			count:    `SELECT count("id") FROM "atxs" WHERE "epoch" = ? AND "rowid" <= ?`,
			maxRowID: `SELECT max("rowid") FROM "atxs"`,
			idRange: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`AND "rowid" <= ? ORDER BY "id" LIMIT ?`,
			recent: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`AND "rowid" <= ? AND "received" >= ? ORDER BY "id" LIMIT ?`,
			recentCount: `SELECT count("id") FROM "atxs" WHERE "epoch" = ? ` +
				`AND "rowid" <= ? AND "received" >= ?`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.all, tc.st.GenSelectAll().String(), "all")
			require.Equal(t, tc.count, tc.st.GenCount().String(), "count")
			require.Equal(t, tc.maxRowID, tc.st.GenSelectMaxRowID().String(), "maxRowID")
			require.Equal(t, tc.idRange, tc.st.GenSelectRange().String(), "idRange")
			require.Equal(t, tc.recent, tc.st.GenSelectRecent().String(), "recent")
			require.Equal(t, tc.recentCount, tc.st.GenRecentCount().String(), "recentCount")
		})
	}
}

func TestSyncedTable_LoadIDs(t *testing.T) {
	var db sql.Database
	type row struct {
		id    string
		epoch int
		ts    int
	}
	rows := []row{
		{"0451cd036aff0367b07590032da827b516b63a4c1b36ea9a253dcf9a7e084980", 1, 100},
		{"0e75d10a8e98a4307dd9d0427dc1d2ebf9e45b602d159ef62c5da95197159844", 1, 110},
		{"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b", 2, 120},
		{"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55", 2, 150},
		{"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1", 2, 180},
		{"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b", 3, 190},
		{"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23", 3, 220},
	}

	insertRows := func(rows []row) {
		for _, r := range rows {
			_, err := db.Exec("insert into atxs (id, epoch, received) values (?, ?, ?)",
				func(stmt *sql.Statement) {
					stmt.BindBytes(1, util.FromHex(r.id))
					stmt.BindInt64(2, int64(r.epoch))
					stmt.BindInt64(3, int64(r.ts))
				}, nil)
			require.NoError(t, err)
		}
	}

	initDB := func() {
		db = sql.InMemoryTest(t)
		_, err := db.Exec(`create table atxs (
			id char(32) not null primary key,
			epoch int,
			received int)`, nil, nil)
		require.NoError(t, err)
		insertRows(rows)
	}

	mkDecode := func(ids *[]string) func(stmt *sql.Statement) bool {
		return func(stmt *sql.Statement) bool {
			id := make(rangesync.KeyBytes, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, id)
			*ids = append(*ids, id.String())
			return true
		}
	}

	loadCount := func(sts *sqlstore.SyncedTableSnapshot) int {
		count, err := sts.LoadCount(db)
		require.NoError(t, err)
		return count
	}

	loadIDs := func(sts *sqlstore.SyncedTableSnapshot) []string {
		var ids []string
		require.NoError(t, sts.Load(db, mkDecode(&ids)))
		return ids
	}

	loadIDsSince := func(stsNew, stsOld *sqlstore.SyncedTableSnapshot) []string {
		var ids []string
		require.NoError(t, stsNew.LoadSinceSnapshot(db, stsOld, mkDecode(&ids)))
		return ids
	}

	loadIDRange := func(sts *sqlstore.SyncedTableSnapshot, from rangesync.KeyBytes, limit int) []string {
		var ids []string
		require.NoError(t, sts.LoadRange(db, from, limit, mkDecode(&ids)))
		return ids
	}

	loadRecentCount := func(
		sts *sqlstore.SyncedTableSnapshot,
		ts int64,
	) int {
		count, err := sts.LoadRecentCount(db, ts)
		require.NoError(t, err)
		return count
	}

	loadRecent := func(
		sts *sqlstore.SyncedTableSnapshot,
		from rangesync.KeyBytes,
		limit int,
		ts int64,
	) []string {
		var ids []string
		require.NoError(t, sts.LoadRecent(db, from, limit, ts, mkDecode(&ids)))
		return ids
	}

	t.Run("no filter", func(t *testing.T) {
		initDB()

		st := &sqlstore.SyncedTable{
			TableName:       "atxs",
			IDColumn:        "id",
			TimestampColumn: "received",
		}

		sts1, err := st.Snapshot(db)
		require.NoError(t, err)

		require.Equal(t, 7, loadCount(sts1))
		require.ElementsMatch(t,
			[]string{
				"0451cd036aff0367b07590032da827b516b63a4c1b36ea9a253dcf9a7e084980",
				"0e75d10a8e98a4307dd9d0427dc1d2ebf9e45b602d159ef62c5da95197159844",
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			},
			loadIDs(sts1))

		fromID := util.FromHex("1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55")
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			},
			loadIDRange(sts1, fromID, 100))

		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			}, loadIDRange(sts1, fromID, 2))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			}, loadRecent(sts1, fromID, 3, 180))
		require.Equal(t, 3, loadRecentCount(sts1, 180))

		insertRows([]row{
			{"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69", 2, 300},
		})

		// the new row is not included in the first snapshot
		require.Equal(t, 7, loadCount(sts1))
		require.ElementsMatch(t,
			[]string{
				"0451cd036aff0367b07590032da827b516b63a4c1b36ea9a253dcf9a7e084980",
				"0e75d10a8e98a4307dd9d0427dc1d2ebf9e45b602d159ef62c5da95197159844",
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			}, loadIDs(sts1))
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			},
			loadIDRange(sts1, fromID, 100))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			},
			loadRecent(sts1, fromID, 3, 180))

		sts2, err := st.Snapshot(db)
		require.NoError(t, err)

		require.Equal(t, 8, loadCount(sts2))
		require.ElementsMatch(t,
			[]string{
				"0451cd036aff0367b07590032da827b516b63a4c1b36ea9a253dcf9a7e084980",
				"0e75d10a8e98a4307dd9d0427dc1d2ebf9e45b602d159ef62c5da95197159844",
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDs(sts2))
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDRange(sts2, fromID, 100))
		require.ElementsMatch(t,
			[]string{
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDsSince(sts2, sts1))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadRecent(sts2, fromID, 4, 180))
		require.ElementsMatch(t,
			[]string{
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadRecent(sts2,
				util.FromHex("2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b"),
				4, 180))
		require.ElementsMatch(t,
			[]string{
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
			},
			loadRecent(sts2,
				util.FromHex("2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b"),
				2, 180))
	})

	t.Run("filter", func(t *testing.T) {
		initDB()
		st := &sqlstore.SyncedTable{
			TableName:       "atxs",
			IDColumn:        "id",
			TimestampColumn: "received",
			Filter:          expr.MustParse("epoch = ?"),
			Binder: func(stmt *sql.Statement) {
				stmt.BindInt64(1, 2)
			},
		}

		sts1, err := st.Snapshot(db)
		require.NoError(t, err)

		require.Equal(t, 3, loadCount(sts1))
		require.ElementsMatch(t,
			[]string{
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadIDs(sts1))

		fromID := util.FromHex("1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55")
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadIDRange(sts1, fromID, 100))
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
			}, loadIDRange(sts1, fromID, 1))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadRecent(sts1, fromID, 1, 180))

		insertRows([]row{
			{"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69", 2, 300},
		})

		// the new row is not included in the first snapshot
		require.Equal(t, 3, loadCount(sts1))
		require.ElementsMatch(t,
			[]string{
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadIDs(sts1))
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadIDRange(sts1, fromID, 100))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadRecent(sts1, fromID, 1, 180))

		sts2, err := st.Snapshot(db)
		require.NoError(t, err)

		require.Equal(t, 4, loadCount(sts2))
		require.ElementsMatch(t,
			[]string{
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDs(sts2))
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDRange(sts2, fromID, 100))
		require.ElementsMatch(t,
			[]string{
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDsSince(sts2, sts1))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadRecent(sts2, fromID, 2, 180))
		require.ElementsMatch(t,
			[]string{
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
			},
			loadRecent(sts2, fromID, 1, 180))
	})
}
