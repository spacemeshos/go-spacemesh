package rewards

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Add reward to the database.
func Add(db sql.Executor, reward *types.Reward) error {
	if _, err := db.Exec(`insert into rewards (coinbase, layer, total_reward, lyr_reward) values (?1, ?2, ?3, ?4);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, reward.Coinbase[:])
			stmt.BindInt64(2, int64(reward.Layer.Uint32()))
			stmt.BindInt64(3, int64(reward.TotalReward))
			stmt.BindInt64(4, int64(reward.LayerReward))
		}, nil); err != nil {
		return fmt.Errorf("insert %+x: %w", reward, err)
	}
	return nil
}

// RevertToLayer reverts the rewards to the specified layer.
func RevertToLayer(db sql.Executor, revertTo types.LayerID) error {
	if _, err := db.Exec(`delete from rewards where layer > ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(revertTo.Uint32()))
		}, nil); err != nil {
		return fmt.Errorf("revert %v: %w", revertTo, err)
	}
	return nil
}

// GetRewards get rewards from all layers for the coinbase address.
func GetRewards(db sql.Executor, coinbase types.Address) (rst []*types.Reward, err error) {
	_, err = db.Exec("select layer, total_reward, lyr_reward from rewards where coinbase = ?1 order by layer;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, coinbase[:])
		}, func(stmt *sql.Statement) bool {
			reward := &types.Reward{
				Coinbase:    coinbase,
				Layer:       types.NewLayerID(uint32(stmt.ColumnInt64(0))),
				TotalReward: uint64(stmt.ColumnInt64(1)),
				LayerReward: uint64(stmt.ColumnInt64(2)),
			}
			rst = append(rst, reward)
			return true
		})
	return
}
