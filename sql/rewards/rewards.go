package rewards

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	coinbaseReward = iota + 1
	smesherReward
)

// Add reward to the database.
func Add(db sql.Executor, lid types.LayerID, reward *types.AnyReward) error {
	if _, err := db.Exec(`insert into rewards 
			(smesher, coinbase, layer, total_reward, layer_reward) 
			values (?1, ?2, ?3, ?4, ?5) 
		on conflict(smesher,layer) 
			do update set 
				total_reward=add_uint64(total_reward, ?4),
		 		layer_reward=add_uint64(layer_reward, ?5);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, reward.SmesherID.ToBytes())
			stmt.BindBytes(2, reward.Address[:])
			stmt.BindInt64(3, int64(lid.Uint32()))
			stmt.BindInt64(4, int64(reward.Amount))
			stmt.BindInt64(5, int64(reward.LayerReward))
		}, nil); err != nil {
		return fmt.Errorf("insert %s %+x: %w", lid, reward, err)
	}
	return nil
}

// order of fields - smesher, coinbase, layer, total_reward, layer_reward .
func decodeReward(stmt *sql.Statement) (types.Reward, error) {
	key := make([]byte, stmt.ColumnLen(0))
	stmt.ColumnBytes(0, key)
	nodeid, err := types.BytesToNodeID(key)
	if err != nil {
		return types.Reward{}, fmt.Errorf("invalid nodeid %x: %w", key, err)
	}
	reward := types.Reward{
		Layer:               types.NewLayerID(uint32(stmt.ColumnInt64(2))),
		TotalReward:         uint64(stmt.ColumnInt64(3)),
		LayerRewardEstimate: uint64(stmt.ColumnInt64(4)),
		SmesherID:           *nodeid,
	}
	stmt.ColumnBytes(1, reward.Coinbase[:])
	return reward, nil
}

// FilterByCoinbase filters rewards from all layers by coinbase address.
func FilterByCoinbase(db sql.Executor, address types.Address) (rst []types.Reward, err error) {
	if _, err := db.Exec("select smesher, coinbase, layer, total_reward, layer_reward from rewards where coinbase = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, address[:])
		}, func(stmt *sql.Statement) bool {
			var reward types.Reward
			reward, err = decodeReward(stmt)
			if err != nil {
				return false
			}
			rst = append(rst, reward)
			return true
		},
	); err != nil {
		return nil, err
	}
	return rst, err
}

// FilterBySmesher filters rewards from all layers by smesher address.
func FilterBySmesher(db sql.Executor, address []byte) (rst []types.Reward, err error) {
	if _, err := db.Exec("select smesher, coinbase, layer, total_reward, layer_reward from rewards where smesher = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, address[:])
		}, func(stmt *sql.Statement) bool {
			var reward types.Reward
			reward, err = decodeReward(stmt)
			if err != nil {
				return false
			}
			rst = append(rst, reward)
			return true
		},
	); err != nil {
		return nil, err
	}
	return rst, err
}
