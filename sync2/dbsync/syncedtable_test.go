package dbsync

import (
	"testing"

	rsql "github.com/rqlite/sql"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

func parseSQLExpr(t *testing.T, s string) rsql.Expr {
	expr, err := rsql.ParseExprString(s)
	require.NoError(t, err)
	return expr
}

func TestSyncedTable_GenSQL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		st       SyncedTable
		allRC    string
		maxRowID string
		IDs      string
		IDsRC    string
		Recent   string
	}{
		{
			name: "no filter",
			st: SyncedTable{
				TableName:       "atxs",
				IDColumn:        "id",
				TimestampColumn: "received",
			},
			allRC:    `SELECT "id" FROM "atxs" WHERE "rowid" <= ?`,
			maxRowID: `SELECT max("rowid") FROM "atxs"`,
			IDs:      `SELECT "id" FROM "atxs" WHERE "id" >= ? ORDER BY "id" LIMIT ?`,
			IDsRC: `SELECT "id" FROM "atxs" WHERE "id" >= ? AND "rowid" <= ? ` +
				`ORDER BY "id" LIMIT ?`,
			Recent: `SELECT "id" FROM "atxs" WHERE "id" >= ? AND "rowid" <= ? ` +
				`AND "received" >= ? ORDER BY "id" LIMIT ?`,
		},
		{
			name: "filter",
			st: SyncedTable{
				TableName:       "atxs",
				IDColumn:        "id",
				Filter:          parseSQLExpr(t, "epoch = ?"),
				TimestampColumn: "received",
			},
			allRC:    `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "rowid" <= ?`,
			maxRowID: `SELECT max("rowid") FROM "atxs"`,
			IDs: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`ORDER BY "id" LIMIT ?`,
			IDsRC: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`AND "rowid" <= ? ORDER BY "id" LIMIT ?`,
			Recent: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`AND "rowid" <= ? AND "received" >= ? ORDER BY "id" LIMIT ?`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.allRC, tc.st.genSelectAllRowIDCutoff().String())
			require.Equal(t, tc.maxRowID, tc.st.genSelectMaxRowID().String())
			require.Equal(t, tc.IDs, tc.st.genSelectIDRange().String())
			require.Equal(t, tc.IDsRC, tc.st.genSelectIDRangeWithRowIDCutoff().String())
			require.Equal(t, tc.Recent, tc.st.genSelectRecentRowIDCutoff().String())
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
			id := make(types.KeyBytes, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, id)
			*ids = append(*ids, id.String())
			return true
		}
	}

	loadIDs := func(sts *SyncedTableSnapshot) []string {
		var ids []string
		require.NoError(t, sts.loadIDs(db, mkDecode(&ids)))
		return ids
	}

	loadIDsSince := func(stsNew, stsOld *SyncedTableSnapshot) []string {
		var ids []string
		require.NoError(t, stsNew.loadIDsSince(db, stsOld, mkDecode(&ids)))
		return ids
	}

	loadIDRange := func(sts *SyncedTableSnapshot, from types.KeyBytes, limit int) []string {
		var ids []string
		require.NoError(t, sts.loadIDRange(db, from, limit, mkDecode(&ids)))
		return ids
	}

	loadRecentCount := func(
		sts *SyncedTableSnapshot,
		ts int64,
	) int {
		count, err := sts.loadRecentCount(db, ts)
		require.NoError(t, err)
		return count
	}

	loadRecent := func(
		sts *SyncedTableSnapshot,
		from types.KeyBytes,
		limit int,
		ts int64,
	) []string {
		var ids []string
		require.NoError(t, sts.loadRecent(db, from, limit, ts, mkDecode(&ids)))
		return ids
	}

	t.Run("no filter", func(t *testing.T) {
		initDB()

		st := &SyncedTable{
			TableName:       "atxs",
			IDColumn:        "id",
			TimestampColumn: "received",
		}

		sts1, err := st.snapshot(db)
		require.NoError(t, err)

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

		sts2, err := st.snapshot(db)
		require.NoError(t, err)

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
		st := &SyncedTable{
			TableName:       "atxs",
			IDColumn:        "id",
			TimestampColumn: "received",
			Filter:          parseSQLExpr(t, "epoch = ?"),
			Binder: func(stmt *sql.Statement) {
				stmt.BindInt64(1, 2)
			},
		}

		sts1, err := st.snapshot(db)
		require.NoError(t, err)

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

		sts2, err := st.snapshot(db)
		require.NoError(t, err)

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
