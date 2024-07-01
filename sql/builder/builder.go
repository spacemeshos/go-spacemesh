package builder

import (
	"fmt"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type token string

const (
	Eq    token = "="
	NotEq token = "!="
	Gt    token = ">"
	Gte   token = ">="
	Lt    token = "<"
	Lte   token = "<="
	In    token = "in"
)

type operator string

const (
	And operator = "and"
	Or  operator = "or"
)

type field string

const (
	Epoch     field = "epoch"
	Smesher   field = "pubkey"
	Coinbase  field = "coinbase"
	Id        field = "id"
	Layer     field = "layer"
	Address   field = "address"
	Principal field = "principal"
)

type modifier string

const (
	Offset  modifier = "offset"
	Limit   modifier = "limit"
	OrderBy modifier = "order by"
	GroupBy modifier = "group by"
)

type Op struct {
	// Prefix will be added before field name
	Prefix string
	Field  field
	Token  token
	// Value will be type casted to one the expected types.
	// Operation will panic if it doesn't match any of expected.
	Value any

	Group         []Op
	GroupOperator operator
}

type Modifier struct {
	Key modifier
	// Value will be type casted to one the expected types.
	// Modifier will panic if it doesn't match any of expected.
	Value any
}

type Operations struct {
	Filter    []Op
	Modifiers []Modifier
	StartWith string
}

func FilterEpochOnly(publish types.EpochID) Operations {
	return Operations{
		Filter: []Op{
			{Field: Epoch, Token: Eq, Value: publish},
		},
	}
}

func FilterFrom(operations Operations) string {
	var queryBuilder strings.Builder

	bindIndex := 1
	for i, op := range operations.Filter {
		if i == 0 {
			if operations.StartWith != "" {
				queryBuilder.WriteString(" " + operations.StartWith)
			} else {
				queryBuilder.WriteString(" where")
			}
		} else {
			queryBuilder.WriteString(" and")
		}

		if len(op.Group) > 0 {
			queryBuilder.WriteString(" (")
			for k, groupOp := range op.Group {
				if k != 0 {
					queryBuilder.WriteString(fmt.Sprintf(" %s", op.GroupOperator))
				}
				if groupOp.Token == In {
					values, ok := groupOp.Value.([][]byte)
					if !ok {
						panic("value for 'In' token must be a slice of []byte")
					}
					params := make([]string, len(values))
					for j := range values {
						params[j] = fmt.Sprintf("?%d", bindIndex)
						bindIndex++
					}
					fmt.Fprintf(&queryBuilder, " %s%s %s (%s)", groupOp.Prefix, groupOp.Field, groupOp.Token,
						strings.Join(params, ", "))
				} else {
					fmt.Fprintf(&queryBuilder, " %s%s %s ?%d", groupOp.Prefix, groupOp.Field, groupOp.Token, bindIndex)
					bindIndex++
				}
			}
			queryBuilder.WriteString(" )")
		} else {
			if op.Token == In {
				values, ok := op.Value.([][]byte)
				if !ok {
					panic("value for 'In' token must be a slice of []byte")
				}
				params := make([]string, len(values))
				for j := range values {
					params[j] = fmt.Sprintf("?%d", bindIndex)
					bindIndex++
				}
				fmt.Fprintf(&queryBuilder, " %s%s %s (%s)", op.Prefix, op.Field, op.Token, strings.Join(params, ", "))
			} else {
				fmt.Fprintf(&queryBuilder, " %s%s %s ?%d", op.Prefix, op.Field, op.Token, bindIndex)
				bindIndex++
			}
		}
	}

	for _, m := range operations.Modifiers {
		queryBuilder.WriteString(fmt.Sprintf(" %s %v", string(m.Key), m.Value))
	}
	return queryBuilder.String()
}

func BindingsFrom(operations Operations) sql.Encoder {
	return func(stmt *sql.Statement) {
		bindIndex := 1
		for _, op := range operations.Filter {
			if len(op.Group) > 0 {
				for _, groupOp := range op.Group {
					bindIndex = bindValue(stmt, bindIndex, groupOp.Value)
				}
			} else {
				bindIndex = bindValue(stmt, bindIndex, op.Value)
			}
		}
	}
}

func bindValue(stmt *sql.Statement, bindIndex int, value any) int {
	switch val := value.(type) {
	case int64:
		stmt.BindInt64(bindIndex, val)
		bindIndex++
	case []byte:
		stmt.BindBytes(bindIndex, val)
		bindIndex++
	case types.EpochID:
		stmt.BindInt64(bindIndex, int64(val))
		bindIndex++
	case [][]byte:
		for _, v := range val {
			stmt.BindBytes(bindIndex, v)
			bindIndex++
		}
	default:
		panic(fmt.Sprintf("unexpected type %T", value))
	}

	return bindIndex
}
