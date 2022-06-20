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
	Address    *types.Address
	Start, End *types.LayerID
	TID        *types.TransactionID
}

func (f *ResultsFilter) hasWhere() bool {
	return f.Address != nil || f.Start != nil || f.End != nil || f.TID != nil
}

func (f *ResultsFilter) query() string {
	var q strings.Builder
	q.WriteString(`
		select distinct id, tx, header, result 
		from transactions
		join transactions_results rst on id=rst.tid
		left join transactions_results_addresses addr on id=addr.tid
	`)
	if f.hasWhere() {
		q.WriteString(" where")
	}
	i := 1
	if f.Address != nil {
		q.WriteString(" address = ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	if f.Start != nil {
		if i != 1 {
			q.WriteString(" and")
		}
		q.WriteString(" layer >= ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	if f.End != nil {
		if i != 1 {
			q.WriteString(" and")
		}
		q.WriteString(" layer <= ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	if f.TID != nil {
		if i != 1 {
			q.WriteString(" and")
		}
		q.WriteString(" id = ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	q.WriteString("order by layer, id")
	q.WriteString(";")
	return q.String()
}

func (f *ResultsFilter) binding(stmt *sql.Statement) {
	position := 1
	if f.Address != nil {
		stmt.BindBytes(position, f.Address[:])
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
		if stmt.ColumnLen(2) > 0 {
			tx.TxHeader = &types.TxHeader{}
			_, ierr = codec.DecodeFrom(stmt.ColumnReader(2), tx.TxHeader)
			if ierr != nil {
				return false
			}
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
