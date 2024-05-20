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
)

type Op struct {
	// Prefix will be added before field name
	Prefix string
	Field  field
	Token  token
	// Value will be type casted to one the expected types.
	// Operation will panic if it doesn't match any of expected.
	Value any
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

		if op.Token == In {
			values, ok := op.Value.([][]byte)
			if !ok {
				panic("value for 'In' token must be a slice of []byte")
			}
			placeholders := make([]string, len(values))
			for j := range values {
				placeholders[j] = fmt.Sprintf("?%d", bindIndex)
				bindIndex++
			}
			fmt.Fprintf(&queryBuilder, " %s%s %s (%s)", op.Prefix, op.Field, op.Token, strings.Join(placeholders, ", "))
		} else {
			fmt.Fprintf(&queryBuilder, " %s%s %s ?%d", op.Prefix, op.Field, op.Token, bindIndex)
			bindIndex++
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
			switch value := op.Value.(type) {
			case int64:
				stmt.BindInt64(bindIndex, value)
				bindIndex++
			case []byte:
				stmt.BindBytes(bindIndex, value)
				bindIndex++
			case types.EpochID:
				stmt.BindInt64(bindIndex, int64(value))
				bindIndex++
			case [][]byte:
				for _, v := range value {
					stmt.BindBytes(bindIndex, v)
					bindIndex++
				}
			default:
				panic(fmt.Sprintf("unexpected type %T", value))
			}
		}
	}
}
