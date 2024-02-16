package builder

import (
	"fmt"
	"strconv"
	"strings"

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
	And   token = "and"
	Where token = "where"
)

type field string

const (
	Epoch    field = "epoch"
	Smesher  field = "pubkey"
	Coinbase field = "coinbase"
	Id       field = "id"
	Offset   field = "offset"
	Limit    field = "limit"
	OrderBy  field = "order by"
)

type Op struct {
	Field field
	Token token
	// Value will be type casted to one the expected types.
	// Operation will panic if it doesn't match any of expected.
	Value any
}

type Operations struct {
	Filter []Op
	Other  []Op
}

func FilterFrom(operations Operations) string {
	var queryBuilder strings.Builder

	for i, op := range operations.Filter {
		if i == 0 {
			queryBuilder.WriteString(" where")
		} else {
			queryBuilder.WriteString(" and")
		}
		queryBuilder.WriteString(" " + string(op.Field) + " " + string(op.Token) + " ?" + strconv.Itoa(i+1))
	}

	for _, op := range operations.Other {
		queryBuilder.WriteString(fmt.Sprintf(" %s %v", string(op.Field), op.Value))
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
			default:
				panic(fmt.Sprintf("unexpected type %T", value))
			}
		}
	}
}
