package sql

import (
	"fmt"

	sqlite "github.com/go-llsqlite/crawshaw"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
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
	// function to prune active set from old ballots
	if err := conn.CreateFunction("prune_actives", true, 1, func(ctx sqlite.Context, values ...sqlite.Value) {
		var ballot types.Ballot
		if err := codec.Decode(values[0].Blob(), &ballot); err != nil {
			ctx.ResultError(err)
		} else {
			ballot.ActiveSet = nil
			if blob, err := codec.Encode(&ballot); err != nil {
				ctx.ResultError(err)
			} else {
				ctx.ResultBlob(blob)
			}
		}
	}, nil, nil); err != nil {
		return fmt.Errorf("registering prune_actives: %w", err)
	}
	return nil
}
