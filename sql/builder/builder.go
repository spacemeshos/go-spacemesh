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
)

type field string

const (
	Epoch    field = "epoch"
	Smesher  field = "pubkey"
	Coinbase field = "coinbase"
	Id       field = "id"
	Layer    field = "layer"
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

	for i, op := range operations.Filter {
		if i == 0 {
			queryBuilder.WriteString(" where")
		} else {
			queryBuilder.WriteString(" and")
		}
		fmt.Fprintf(&queryBuilder, " %s%s %s ?%d", op.Prefix, op.Field, op.Token, i+1)
	}

	for _, m := range operations.Modifiers {
		queryBuilder.WriteString(fmt.Sprintf(" %s %v", string(m.Key), m.Value))
	}

	return queryBuilder.String()
}

func BindingsFrom(operations Operations) sql.Encoder {
	return func(stmt *sql.Statement) {
		for i, op := range operations.Filter {
			switch value := op.Value.(type) {
			case int64:
				stmt.BindInt64(i+1, value)
			case []byte:
				stmt.BindBytes(i+1, value)
			case types.EpochID:
				stmt.BindInt64(i+1, int64(value))
			default:
				panic(fmt.Sprintf("unexpected type %T", value))
			}
		}
	}
}
