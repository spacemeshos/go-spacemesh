package sqlstore

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/expr"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// Binder is a function that binds filter expression parameters to a SQL statement.
type Binder func(s *sql.Statement)

// SyncedTable represents a table that can be used with SQLIDStore.
type SyncedTable struct {
	// The name of the table.
	TableName string
	// The name of the ID column.
	IDColumn string
	// The name of the timestamp column.
	TimestampColumn string
	// The filter expression.
	Filter expr.Expr
	// The binder function for the bind parameters appearing in the filter expression.
	Binder Binder
}

// genSelectMaxRowID generates a SELECT statement that returns the maximum rowid in the
// table.
func (st *SyncedTable) genSelectMaxRowID() expr.Statement {
	return expr.Select(expr.Call("max", expr.Ident("rowid"))).
		From(expr.TableSource(st.TableName)).
		Get()
}

// rowIDCutoff returns an expression that represents a rowid cutoff, that is, limits the
// rowid to be less than or equal to a bind parameter.
func (st *SyncedTable) rowIDCutoff() expr.Expr {
	return expr.Op(expr.Ident("rowid"), expr.LE, expr.Bind())
}

// timestampCutoff returns an expression that represents a timestamp cutoff, that is, limits the
// timestamp to be greater than or equal to a bind parameter.
func (st *SyncedTable) timestampCutoff() expr.Expr {
	return expr.Op(expr.Ident(st.TimestampColumn), expr.GE, expr.Bind())
}

// genSelectAll generates a SELECT statement that returns all the rows in the table
// satisfying the filter expression and the rowid cutoff.
func (st *SyncedTable) genSelectAll() expr.Statement {
	return expr.Select(expr.Ident(st.IDColumn)).
		From(expr.TableSource(st.TableName)).
		Where(expr.MaybeAnd(st.Filter, st.rowIDCutoff())).
		Get()
}

// genCount generates a SELECT statement that returns the number of rows in the table
// satisfying the filter expression and the rowid cutoff.
func (st *SyncedTable) genCount() expr.Statement {
	return expr.Select(expr.Call("count", expr.Ident(st.IDColumn))).
		From(expr.TableSource(st.TableName)).
		Where(expr.MaybeAnd(st.Filter, st.rowIDCutoff())).
		Get()
}

// genSelectAllSinceSnapshot generates a SELECT statement that returns all the rows in the
// table satisfying the filter expression that have rowid between the specified min and
// max parameter values, inclusive.
func (st *SyncedTable) genSelectAllSinceSnapshot() expr.Statement {
	return expr.Select(expr.Ident(st.IDColumn)).
		From(expr.TableSource(st.TableName)).
		Where(expr.MaybeAnd(
			st.Filter,
			expr.Between(expr.Ident("rowid"), expr.Bind(), expr.Bind()))).
		Get()
}

// genSelectRange generates a SELECT statement that returns the rows in the table
// satisfying the filter expression, the rowid cutoff and the specified ID range.
func (st *SyncedTable) genSelectRange() expr.Statement {
	return expr.Select(expr.Ident(st.IDColumn)).
		From(expr.TableSource(st.TableName)).
		Where(expr.MaybeAnd(
			st.Filter,
			expr.Op(expr.Ident(st.IDColumn), expr.GE, expr.Bind()),
			st.rowIDCutoff())).
		OrderBy(expr.Asc(expr.Ident(st.IDColumn))).
		Limit(expr.Bind()).
		Get()
}

// genRecentCount generates a SELECT statement that returns the number of rows in the table
// added starting with the specified timestamp, taking into account the filter expression
// and the rowid cutoff.
func (st *SyncedTable) genRecentCount() expr.Statement {
	return expr.Select(expr.Call("count", expr.Ident(st.IDColumn))).
		From(expr.TableSource(st.TableName)).
		Where(expr.MaybeAnd(st.Filter, st.rowIDCutoff(), st.timestampCutoff())).
		Get()
}

// genRecentCount generates a SELECT statement that returns the rows in the table added
// starting with the specified timestamp, taking into account the filter expression and
// the rowid cutoff.
func (st *SyncedTable) genSelectRecent() expr.Statement {
	return expr.Select(expr.Ident(st.IDColumn)).
		From(expr.TableSource(st.TableName)).
		Where(expr.MaybeAnd(
			st.Filter,
			expr.Op(expr.Ident(st.IDColumn), expr.GE, expr.Bind()),
			st.rowIDCutoff(), st.timestampCutoff())).
		OrderBy(expr.Asc(expr.Ident(st.IDColumn))).
		Limit(expr.Bind()).
		Get()
}

// loadMaxRowID returns the max rowid in the table.
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

// Snaptshot creates a snapshot of the table based on its current max rowid value.
func (st *SyncedTable) Snapshot(db sql.Executor) (*SyncedTableSnapshot, error) {
	maxRowID, err := st.loadMaxRowID(db)
	if err != nil {
		return nil, err
	}
	return &SyncedTableSnapshot{st, maxRowID}, nil
}

// SyncedTableSnapshot represents a snapshot of an append-only table.
// The snapshotting is relies on rowids of the table rows never decreasing
// as new rows are added.
// Each snapshot inherits filter expression from the table, so all the rows relevant to
// the snapshot are always filtered using that expression, if it's specified.
type SyncedTableSnapshot struct {
	*SyncedTable
	maxRowID int64
}

// Load loads all the table rows belonging to a snapshot.
func (sts *SyncedTableSnapshot) Load(
	db sql.Executor,
	dec func(stmt *sql.Statement) bool,
) error {
	_, err := db.Exec(
		sts.genSelectAll().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			stmt.BindInt64(stmt.BindParamCount(), sts.maxRowID)
		},
		dec)
	return err
}

// LoadCount returns the number of rows in the snapshot.
func (sts *SyncedTableSnapshot) LoadCount(
	db sql.Executor,
) (int, error) {
	var count int
	_, err := db.Exec(
		sts.genCount().String(),
		func(stmt *sql.Statement) {
			if sts.Binder != nil {
				sts.Binder(stmt)
			}
			stmt.BindInt64(stmt.BindParamCount(), sts.maxRowID)
		},
		func(stmt *sql.Statement) bool {
			count = stmt.ColumnInt(0)
			return true
		})
	return count, err
}

// LoadSinceSnapshot loads rows added since the specified previous snapshot.
func (sts *SyncedTableSnapshot) LoadSinceSnapshot(
	db sql.Executor,
	prev *SyncedTableSnapshot,
	dec func(stmt *sql.Statement) bool,
) error {
	_, err := db.Exec(
		sts.genSelectAllSinceSnapshot().String(),
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

// LoadRange loads ids starting from the specified one.
// limit specifies the maximum number of ids to load.
func (sts *SyncedTableSnapshot) LoadRange(
	db sql.Executor,
	fromID rangesync.KeyBytes,
	limit int,
	dec func(stmt *sql.Statement) bool,
) error {
	_, err := db.Exec(
		sts.genSelectRange().String(),
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

var errNoTimestampColumn = errors.New("no timestamp column")

// LoadRecentCount returns the number of rows added since the specified timestamp.
func (sts *SyncedTableSnapshot) LoadRecentCount(
	db sql.Executor,
	since int64,
) (int, error) {
	if sts.TimestampColumn == "" {
		return 0, errNoTimestampColumn
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

// LoadRecent loads rows added since the specified timestamp.
func (sts *SyncedTableSnapshot) LoadRecent(
	db sql.Executor,
	fromID rangesync.KeyBytes,
	limit int,
	since int64,
	dec func(stmt *sql.Statement) bool,
) error {
	if sts.TimestampColumn == "" {
		return errNoTimestampColumn
	}
	_, err := db.Exec(
		sts.genSelectRecent().String(),
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
