package dbsync

import (
	"testing"

	rsql "github.com/rqlite/sql"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// func TestRmme(t *testing.T) {
// 	s, err := rsql.ParseExprString("x.id = ? and y.id = ?")
// 	require.NoError(t, err)
// 	var v bindCountVisitor
// 	require.NoError(t, rsql.Walk(&v, s))
// 	require.Equal(t, 2, v.numBinds)

// 	st, err := rsql.NewParser(strings.NewReader("select max(rowidx) from foo")).ParseStatement()
// 	require.NoError(t, err)
// 	spew.Config.DisableMethods = true
// 	spew.Config.DisablePointerAddresses = true
// 	defer func() {
// 		spew.Config.DisableMethods = false
// 		spew.Config.DisablePointerAddresses = false
// 	}()
// 	t.Logf("s: %s\n", spew.Sdump(st))
// }

func parseSQLExpr(t *testing.T, s string) rsql.Expr {
	expr, err := rsql.ParseExprString(s)
	require.NoError(t, err)
	return expr
}

func TestSyncedTable_GenSQL(t *testing.T) {
	for _, tc := range []struct {
		name           string
		st             SyncedTable
		selectAllRC    string
		selectMaxRowID string
		selectIDs      string
		selectIDsRC    string
	}{
		{
			name: "no filter",
			st: SyncedTable{
				TableName: "atxs",
				IDColumn:  "id",
			},
			selectAllRC:    `SELECT "id" FROM "atxs" WHERE "rowid" <= ?`,
			selectMaxRowID: `SELECT max("rowid") FROM "atxs"`,
			selectIDs:      `SELECT "id" FROM "atxs" WHERE "id" >= ? ORDER BY "id" LIMIT ?`,
			selectIDsRC: `SELECT "id" FROM "atxs" WHERE "id" >= ? AND "rowid" <= ? ` +
				`ORDER BY "id" LIMIT ?`,
		},
		{
			name: "filter",
			st: SyncedTable{
				TableName: "atxs",
				IDColumn:  "id",
				Filter:    parseSQLExpr(t, "epoch = ?"),
			},
			selectAllRC:    `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "rowid" <= ?`,
			selectMaxRowID: `SELECT max("rowid") FROM "atxs"`,
			selectIDs: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`ORDER BY "id" LIMIT ?`,
			selectIDsRC: `SELECT "id" FROM "atxs" WHERE "epoch" = ? AND "id" >= ? ` +
				`AND "rowid" <= ? ORDER BY "id" LIMIT ?`,
		},
	} {
		require.Equal(t, tc.selectAllRC, tc.st.genSelectAllRowIDCutoff().String())
		require.Equal(t, tc.selectMaxRowID, tc.st.genSelectMaxRowID().String())
		require.Equal(t, tc.selectIDs, tc.st.genSelectIDRange().String())
		require.Equal(t, tc.selectIDsRC, tc.st.genSelectIDRangeWithRowIDCutoff().String())
	}
}

func TestSyncedTable_LoadIDs(t *testing.T) {
	var db sql.Database
	type row struct {
		id    string
		epoch int
	}
	rows := []row{
		{"0451cd036aff0367b07590032da827b516b63a4c1b36ea9a253dcf9a7e084980", 1},
		{"0e75d10a8e98a4307dd9d0427dc1d2ebf9e45b602d159ef62c5da95197159844", 1},
		{"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b", 2},
		{"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55", 2},
		{"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1", 2},
		{"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b", 3},
		{"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23", 3},
	}

	insertRows := func(rows []row) {
		for _, r := range rows {
			_, err := db.Exec("insert into atxs (id, epoch) values (?, ?)",
				func(stmt *sql.Statement) {
					stmt.BindBytes(1, util.FromHex(r.id))
					stmt.BindInt64(2, int64(r.epoch))
				}, nil)
			require.NoError(t, err)
		}
	}

	initDB := func() {
		db = sql.InMemoryTest(t)
		_, err := db.Exec("create table atxs (id char(32) not null primary key, epoch int)", nil, nil)
		require.NoError(t, err)
		insertRows(rows)
	}

	loadIDs := func(sts *SyncedTableSnapshot) []string {
		var ids []string
		require.NoError(t, sts.loadIDs(db, func(stmt *sql.Statement) bool {
			id := make(KeyBytes, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, id)
			ids = append(ids, id.String())
			return true
		}))
		return ids
	}

	loadIDRange := func(sts *SyncedTableSnapshot, from KeyBytes, limit int) []string {
		var ids []string
		require.NoError(t, sts.loadIDRange(
			db, from, limit,
			func(stmt *sql.Statement) bool {
				id := make(KeyBytes, stmt.ColumnLen(0))
				stmt.ColumnBytes(0, id)
				ids = append(ids, id.String())
				return true
			}))
		return ids
	}

	t.Run("no filter", func(t *testing.T) {
		initDB()

		st := &SyncedTable{
			TableName: "atxs",
			IDColumn:  "id",
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

		insertRows([]row{
			{"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69", 2},
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
			}, loadIDs(sts2))
		require.ElementsMatch(t,
			[]string{
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
			},
			loadIDRange(sts2, fromID, 100))
	})

	t.Run("filter", func(t *testing.T) {
		initDB()
		st := &SyncedTable{
			TableName: "atxs",
			IDColumn:  "id",
			Filter:    parseSQLExpr(t, "epoch = ?"),
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

		insertRows([]row{
			{"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69", 2},
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
	})
}
