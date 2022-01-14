package rewards

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Reward ...
type Reward struct {
	Address types.Address
	Layer   types.LayerID
	Value   uint64
}

// Add reward to the database.
func Add(db sql.Executor, reward *Reward) error {
	if _, err := db.Exec(`insert into rewards (coinbase, layer, total) values (?1, ?2, ?3) 
					on conflict(coinbase, layer) do update set total=add_uint64(total,?3);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, reward.Address[:])
			stmt.BindInt64(2, int64(reward.Layer.Uint32()))
			stmt.BindInt64(3, int64(reward.Value))
		}, nil); err != nil {
		return fmt.Errorf("insert %s %d: %w", reward.Address, reward.Value, err)
	}
	return nil
}

// CoinbaseTotal ...
func CoinbaseTotal(db sql.Executor, address types.Address) (rst uint64, err error) {
	if _, err := db.Exec("select sum(total) from rewards where coinbase = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, address[:])
		}, func(stmt *sql.Statement) bool {
			rst = uint64(stmt.ColumnInt64(0))
			return true
		},
	); err != nil {
		return 0, err
	}
	return rst, nil
}
