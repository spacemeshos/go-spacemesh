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

func (f *ResultsFilter) query() string {
	var q strings.Builder
	q.WriteString(`
		select distinct id, tx, header, result 
		from transactions
		left join transactions_results_addresses on id=tid
		where result is not null
	`)
	i := 1
	if f.Address != nil {
		q.WriteString(" and address = ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	if f.Start != nil {
		q.WriteString(" and layer >= ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	if f.End != nil {
		q.WriteString(" and layer <= ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	if f.TID != nil {
		q.WriteString(" and id = ?")
		q.WriteString(strconv.Itoa(i))
		i++
	}
	q.WriteString("order by layer, id;")
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
		var tx types.TransactionWithResult

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
