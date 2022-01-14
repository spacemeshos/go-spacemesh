package sql

import (
	"fmt"

	"crawshaw.io/sqlite"
)

func registerFunctions(conn *sqlite.Conn) error {
	if err := conn.CreateFunction("add_uint64", true, 2, func(ctx sqlite.Context, values ...sqlite.Value) {
		ctx.ResultInt64(int64(uint64(values[0].Int64()) + uint64(values[1].Int64())))
	}, nil, nil); err != nil {
		return fmt.Errorf("registering add_uint64: %w", err)
	}
	return nil
}
