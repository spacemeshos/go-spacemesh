package sql

import (
	"fmt"

	"github.com/go-llsqlite/llsqlite"
)

func registerFunctions(conn *sqlite.Conn) error {
	// sqlite doesn't provide native support for uint64,
	// it is a problem if we want to sort items using actual uint64 value
	// or do arithmetic operations on uint64 in database
	// for that we have to add custom functions, another example https://stackoverflow.com/a/8503318
	if err := conn.CreateFunction("add_uint64", true, 2, func(ctx sqlite.Context, values ...sqlite.Value) {
		ctx.ResultInt64(int64(uint64(values[0].Int64()) + uint64(values[1].Int64())))
	}, nil, nil); err != nil {
		return fmt.Errorf("registering add_uint64: %w", err)
	}
	return nil
}
