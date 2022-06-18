package transactions

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// ResultsFilter applies filter on transaction results query.
type ResultsFilter struct {
	Addresses  []types.Address
	Start, End *types.LayerID
	TID        *types.TransactionID
}

func (f *ResultsFilter) query() string {
	var q strings.Builder
	q.WriteString(`
		select distinct id, tx, header, result 
		from transactions
		join transactions_results rst on id=rst.tid
		left join transactions_results_addresses addr on id=addr.tid
	`)
	position := 1
	for i := range f.Addresses {
		if i > 0 {
			q.WriteString(" or")
		}
		q.WriteString(" address = ?")
		q.WriteString(strconv.Itoa(position))
		position++
	}
	if f.Start != nil {
		if position != 1 {
			q.WriteString(" and")
		}
		q.WriteString(" layer >= ?")
		q.WriteString(strconv.Itoa(position))
		position++
	}
	if f.Start != nil {
		if position != 1 {
			q.WriteString(" and")
		}
		q.WriteString(" layer <= ?")
		q.WriteString(strconv.Itoa(position))
		position++
	}
	if f.TID != nil {
		if position != 1 {
			q.WriteString(" and")
		}
		q.WriteString(" id = ?")
		q.WriteString(strconv.Itoa(position))
		position++
	}
	q.WriteString(";")
	return q.String()
}

func (f *ResultsFilter) binding(stmt *sql.Statement) {
	position := 1
	for i := range f.Addresses {
		stmt.BindBytes(position, f.Addresses[i][:])
		position++
	}
	if f.Start != nil {
		stmt.BindInt64(position, int64(f.Start.Value))
		position++
	}
	if f.End != nil {
		stmt.BindInt64(position, int64(f.End.Value))
		position++
	}
	if f.TID != nil {
		stmt.BindBytes(position, f.TID.Bytes())
	}
}

// IterateResults allows to control iteration by the output of `fn`.
func IterateResults(db sql.Executor, filter ResultsFilter, fn func(*types.TransactionWithResult) bool) error {
	var ierr error
	_, err := db.Exec(filter.query(), filter.binding, func(stmt *sql.Statement) bool {
		var (
			tx types.TransactionWithResult
		)
		stmt.ColumnBytes(0, tx.ID[:])
		tx.Raw = make([]byte, stmt.ColumnLen(1))
		stmt.ColumnBytes(1, tx.Raw)
		tx.TxHeader = &types.TxHeader{}
		_, ierr = codec.DecodeFrom(stmt.ColumnReader(2), tx.TxHeader)
		if ierr != nil {
			return false
		}
		_, ierr = codec.DecodeFrom(stmt.ColumnReader(3), &tx.TransactionResult)
		if ierr != nil {
			return false
		}
		return fn(&tx)
	})
	if err == nil {
		err = ierr
	}
	if err != nil {
		return fmt.Errorf("iteration failed %w", err)
	}
	return nil
}
