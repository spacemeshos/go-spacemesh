package dbsync

import (
	"fmt"

	rsql "github.com/rqlite/sql"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Binder func(s *sql.Statement)

type SyncedTable struct {
	TableName string
	IDColumn  string
	Filter    rsql.Expr
	Binder    Binder
}

func (st *SyncedTable) genSelectAll() *rsql.SelectStatement {
	return &rsql.SelectStatement{
		Columns: []*rsql.ResultColumn{
			{Expr: &rsql.Ident{Name: st.IDColumn}},
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

func (st *SyncedTable) genSelectIDRangeWithRowIDCutoff() *rsql.SelectStatement {
	s := st.genSelectIDRange()
	s.WhereExpr = &rsql.BinaryExpr{
		X:  s.WhereExpr,
		Op: rsql.AND,
		Y:  st.rowIDCutoff(),
	}
	return s
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
			stmt.BindBytes(nParams-2, fromID)
			stmt.BindInt64(nParams-1, sts.maxRowID)
			stmt.BindInt64(nParams, int64(limit))
		},
		dec)
	return err
}

// func (st *SyncedTable) bind(s *sql.Statement) int {
// 	ofs := 0
// 	if st.Filter != nil {
// 		var v bindCountVisitor
// 		if err := rsql.Walk(&v, st.Filter); err != nil {
// 			panic("BUG: bad filter: " + err.Error())
// 		}
// 		ofs = v.numBinds
// 		switch {
// 		case ofs == 0 && st.Binder != nil:
// 			panic("BUG: filter has no binds but a binder is passed")
// 		case ofs > 0 && st.Binder == nil:
// 			panic("BUG: filter has binds but no binder is passed")
// 		}
// 		st.Binder(s)
// 	} else if st.Binder != nil {
// 		panic("BUG: there's no filter but there's a binder")
// 	}
// 	return ofs
// }

// type bindCountVisitor struct {
// 	numBinds int
// }

// var _ rsql.Visitor = &bindCountVisitor{}

// func (b *bindCountVisitor) Visit(node rsql.Node) (w rsql.Visitor, err error) {
// 	bExpr, ok := node.(*rsql.BindExpr)
// 	if !ok {
// 		return b, nil
// 	}
// 	if bExpr.Name != "?" {
// 		return nil, fmt.Errorf("bad bind %s: only ? binds are supported", bExpr.Name)
// 	}
// 	b.numBinds++
// 	return nil, nil
// }

// func (b *bindCountVisitor) VisitEnd(node rsql.Node) error { return nil }
