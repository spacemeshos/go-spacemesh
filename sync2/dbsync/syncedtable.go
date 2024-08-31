package dbsync

import (
	"fmt"

	rsql "github.com/rqlite/sql"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Binder func(s *sql.Statement)

type SyncedTable struct {
	TableName       string
	IDColumn        string
	TimestampColumn string
	Filter          rsql.Expr
	Binder          Binder
}

func (st *SyncedTable) genSelectAll() *rsql.SelectStatement {
	return &rsql.SelectStatement{
		Columns: []*rsql.ResultColumn{
			{
				Expr: &rsql.Ident{Name: st.IDColumn},
			},
		},
		Source:    &rsql.QualifiedTableName{Name: &rsql.Ident{Name: st.TableName}},
		WhereExpr: st.Filter,
	}
}

func (st *SyncedTable) genSelectMaxRowID() *rsql.SelectStatement {
	return &rsql.SelectStatement{
		Columns: []*rsql.ResultColumn{
			{
				Expr: &rsql.Call{
					Name: &rsql.Ident{Name: "max"},
					Args: []rsql.Expr{&rsql.Ident{Name: "rowid"}},
				},
			},
		},
		Source: &rsql.QualifiedTableName{Name: &rsql.Ident{Name: st.TableName}},
	}
}

func (st *SyncedTable) genSelectIDRange() *rsql.SelectStatement {
	s := st.genSelectAll()
	where := &rsql.BinaryExpr{
		X:  &rsql.Ident{Name: st.IDColumn},
		Op: rsql.GE,
		Y:  &rsql.BindExpr{Name: "?"},
	}
	if s.WhereExpr != nil {
		s.WhereExpr = &rsql.BinaryExpr{
			X:  s.WhereExpr,
			Op: rsql.AND,
			Y:  where,
		}
	} else {
		s.WhereExpr = where
	}
	s.OrderingTerms = []*rsql.OrderingTerm{
		{X: &rsql.Ident{Name: st.IDColumn}},
	}
	s.LimitExpr = &rsql.BindExpr{Name: "?"}
	return s
}

func (st *SyncedTable) rowIDCutoff() rsql.Expr {
	return &rsql.BinaryExpr{
		X:  &rsql.Ident{Name: "rowid"},
		Op: rsql.LE,
		Y:  &rsql.BindExpr{Name: "?"},
	}
}

func (st *SyncedTable) genSelectAllRowIDCutoff() *rsql.SelectStatement {
	s := st.genSelectAll()
	if s.WhereExpr != nil {
		s.WhereExpr = &rsql.BinaryExpr{
			X:  s.WhereExpr,
			Op: rsql.AND,
			Y:  st.rowIDCutoff(),
		}
	} else {
		s.WhereExpr = st.rowIDCutoff()
	}
	return s
}

func (st *SyncedTable) genSelectAllRowIDCutoffSince() *rsql.SelectStatement {
	s := st.genSelectAll()
	rowIDBetween := &rsql.BinaryExpr{
		X:  &rsql.Ident{Name: "rowid"},
		Op: rsql.BETWEEN,
		Y: &rsql.Range{
			X: &rsql.BindExpr{Name: "?"},
			Y: &rsql.BindExpr{Name: "?"},
		},
	}
	if s.WhereExpr != nil {
		s.WhereExpr = &rsql.BinaryExpr{
			X:  s.WhereExpr,
			Op: rsql.AND,
			Y:  rowIDBetween,
		}
	} else {
		s.WhereExpr = rowIDBetween
	}
	return s
}

func (st *SyncedTable) genSelectIDRangeWithRowIDCutoff() *rsql.SelectStatement {
	s := st.genSelectIDRange()
	s.WhereExpr = &rsql.BinaryExpr{
		X:  s.WhereExpr,
		Op: rsql.AND,
		Y:  st.rowIDCutoff(),
	}
	return s
}

func (st *SyncedTable) genSelectRecentRowIDCutoff() *rsql.SelectStatement {
	s := st.genSelectIDRangeWithRowIDCutoff()
	s.WhereExpr = &rsql.BinaryExpr{
		X:  s.WhereExpr,
		Op: rsql.AND,
		Y: &rsql.BinaryExpr{
			X:  &rsql.Ident{Name: st.TimestampColumn},
			Op: rsql.GE,
			Y:  &rsql.BindExpr{Name: "?"},
		},
	}
	return s
}

func (st *SyncedTable) genRecentCount() *rsql.SelectStatement {
	where := &rsql.BinaryExpr{
		X: &rsql.BinaryExpr{
			X:  &rsql.Ident{Name: "rowid"},
			Op: rsql.LE,
			Y:  &rsql.BindExpr{Name: "?"},
		},
		Op: rsql.AND,
		Y: &rsql.BinaryExpr{
			X:  &rsql.Ident{Name: st.TimestampColumn},
			Op: rsql.GE,
			Y:  &rsql.BindExpr{Name: "?"},
		},
	}
	if st.Filter != nil {
		where = &rsql.BinaryExpr{
			X:  st.Filter,
			Op: rsql.AND,
			Y:  where,
		}
	}
	return &rsql.SelectStatement{
		Columns: []*rsql.ResultColumn{
			{
				Expr: &rsql.Call{
					Name: &rsql.Ident{Name: "count"},
					Args: []rsql.Expr{&rsql.Ident{Name: st.IDColumn}},
				},
			},
		},
		Source:    &rsql.QualifiedTableName{Name: &rsql.Ident{Name: st.TableName}},
		WhereExpr: where,
	}
}

func (st *SyncedTable) loadMaxRowID(db sql.Executor) (maxRowID int64, err error) {
	nRows, err := db.Exec(
		st.genSelectMaxRowID().String(), nil,
		func(st *sql.Statement) bool {
			maxRowID = st.ColumnInt64(0)
			return true
		})
	if nRows != 1 {
		return 0, fmt.Errorf("expected 1 row, got %d", nRows)
	}
	return maxRowID, err
}

func (st *SyncedTable) snapshot(db sql.Executor) (*SyncedTableSnapshot, error) {
	maxRowID, err := st.loadMaxRowID(db)
	if err != nil {
		return nil, err
	}
	return &SyncedTableSnapshot{st, maxRowID}, nil
}

type SyncedTableSnapshot struct {
	*SyncedTable
	maxRowID int64
}

func (sts *SyncedTableSnapshot) loadIDs(
	db sql.Executor,
	dec func(stmt *sql.Statement) bool,
) error {
	_, err := db.Exec(
		sts.genSelectAllRowIDCutoff().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			stmt.BindInt64(stmt.BindParamCount(), sts.maxRowID)
		},
		dec)
	return err
}

func (sts *SyncedTableSnapshot) loadIDsSince(
	db sql.Executor,
	prev *SyncedTableSnapshot,
	dec func(stmt *sql.Statement) bool,
) error {
	_, err := db.Exec(
		sts.genSelectAllRowIDCutoffSince().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			nParams := stmt.BindParamCount()
			stmt.BindInt64(nParams-1, prev.maxRowID+1)
			stmt.BindInt64(nParams, sts.maxRowID)
		},
		dec)
	return err
}

func (sts *SyncedTableSnapshot) loadIDRange(
	db sql.Executor,
	fromID KeyBytes,
	limit int,
	dec func(stmt *sql.Statement) bool,
) error {
	_, err := db.Exec(
		sts.genSelectIDRangeWithRowIDCutoff().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			nParams := stmt.BindParamCount()
			// fmt.Fprintf(os.Stderr, "QQQQQ: STMT: %s\nfromID %s maxRowID %d limit %d\n",
			// 	sts.genSelectIDRangeWithRowIDCutoff().String(),
			// 	fromID.String(), sts.maxRowID, limit)
			stmt.BindBytes(nParams-2, fromID)
			stmt.BindInt64(nParams-1, sts.maxRowID)
			stmt.BindInt64(nParams, int64(limit))
		},
		dec)
	return err
}

func (sts *SyncedTableSnapshot) loadRecentCount(
	db sql.Executor,
	since int64,
) (int, error) {
	if sts.TimestampColumn == "" {
		return 0, fmt.Errorf("no timestamp column")
	}
	var count int
	_, err := db.Exec(
		sts.genRecentCount().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			nParams := stmt.BindParamCount()
			stmt.BindInt64(nParams-1, sts.maxRowID)
			stmt.BindInt64(nParams, since)
		},
		func(stmt *sql.Statement) bool {
			count = stmt.ColumnInt(0)
			return true
		})
	return count, err
}

func (sts *SyncedTableSnapshot) loadRecent(
	db sql.Executor,
	fromID KeyBytes,
	limit int,
	since int64,
	dec func(stmt *sql.Statement) bool,
) error {
	if sts.TimestampColumn == "" {
		return fmt.Errorf("no timestamp column")
	}
	_, err := db.Exec(
		sts.genSelectRecentRowIDCutoff().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			nParams := stmt.BindParamCount()
			stmt.BindBytes(nParams-3, fromID)
			stmt.BindInt64(nParams-2, sts.maxRowID)
			stmt.BindInt64(nParams-1, since)
			stmt.BindInt64(nParams, int64(limit))
		},
		dec)
	return err
}
