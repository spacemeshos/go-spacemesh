package rewards

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

const fullQuery = `select pubkey, coinbase, layer, total_reward, layer_reward from rewards`

type decoderCallback func(*types.Reward, error) bool

func decoder(fn decoderCallback) sql.Decoder {
	return func(stmt *sql.Statement) bool {
		smID := types.NodeID{}
		cbase := types.Address{}
		stmt.ColumnBytes(0, smID[:])
		stmt.ColumnBytes(1, cbase[:])
		reward := &types.Reward{
			SmesherID:   smID,
			Coinbase:    cbase,
			Layer:       types.LayerID(uint32(stmt.ColumnInt64(2))),
			TotalReward: uint64(stmt.ColumnInt64(3)),
			LayerReward: uint64(stmt.ColumnInt64(4)),
		}
		return fn(reward, nil)
	}
}

// Add reward to the database.
func Add(db sql.Executor, reward *types.Reward) error {
	if _, err := db.Exec(`
		insert into rewards (pubkey, coinbase, layer, total_reward, layer_reward) values (?1, ?2, ?3, ?4, ?5)`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, reward.SmesherID[:])
			stmt.BindBytes(2, reward.Coinbase[:])
			stmt.BindInt64(3, int64(reward.Layer.Uint32()))
			stmt.BindInt64(4, int64(reward.TotalReward))
			stmt.BindInt64(5, int64(reward.LayerReward))
		}, nil); err != nil {
		return fmt.Errorf("insert %+x: %w", reward, err)
	}
	return nil
}

// Revert the rewards to the specified layer.
func Revert(db sql.Executor, revertTo types.LayerID) error {
	if _, err := db.Exec(`delete from rewards where layer > ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(revertTo.Uint32()))
		}, nil); err != nil {
		return fmt.Errorf("revert %v: %w", revertTo, err)
	}
	return nil
}

// ListByKey lists rewards from all layers for the specified smesherID and/or coinbase.
func ListByKey(db sql.Executor, coinbase *types.Address, smesherID *types.NodeID) (rst []*types.Reward, err error) {
	var whereClause string
	var binder func(*sql.Statement)
	if coinbase != nil && smesherID != nil {
		whereClause = "pubkey = ?1 and coinbase = ?2"
		binder = func(stmt *sql.Statement) {
			stmt.BindBytes(1, smesherID[:])
			stmt.BindBytes(2, coinbase[:])
		}
	} else if coinbase != nil {
		whereClause = "coinbase = ?1"
		binder = func(stmt *sql.Statement) {
			stmt.BindBytes(1, coinbase[:])
		}
	} else if smesherID != nil {
		whereClause = "pubkey = ?1"
		binder = func(stmt *sql.Statement) {
			stmt.BindBytes(1, smesherID[:])
		}
	} else {
		return nil, errors.New("must specify coinbase and/or smesherID")
	}
	stmt := fmt.Sprintf(
		"%s where %s order by layer;",
		fullQuery, whereClause)
	_, err = db.Exec(stmt, binder, func(stmt *sql.Statement) bool {
		smID := types.NodeID{}
		cbase := types.Address{}
		stmt.ColumnBytes(0, smID[:])
		stmt.ColumnBytes(1, cbase[:])
		reward := &types.Reward{
			SmesherID:   smID,
			Coinbase:    cbase,
			Layer:       types.LayerID(uint32(stmt.ColumnInt64(2))),
			TotalReward: uint64(stmt.ColumnInt64(3)),
			LayerReward: uint64(stmt.ColumnInt64(4)),
		}
		rst = append(rst, reward)
		return true
	})
	return rst, err
}

// ListByCoinbase lists rewards from all layers for the coinbase address.
func ListByCoinbase(db sql.Executor, coinbase types.Address) (rst []*types.Reward, err error) {
	return ListByKey(db, &coinbase, nil)
}

// ListBySmesherId lists rewards from all layers for the smesher ID.
func ListBySmesherId(db sql.Executor, smesherID types.NodeID) (rst []*types.Reward, err error) {
	return ListByKey(db, nil, &smesherID)
}

func IterateRewardsOps(
	db sql.Executor,
	operations builder.Operations,
	fn func(reward *types.Reward) bool,
) error {
	var derr error
	_, err := db.Exec(
		fullQuery+builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		decoder(func(reward *types.Reward, err error) bool {
			if reward != nil {
				fn(reward)
			}
			derr = err
			return derr == nil
		}),
	)
	if err != nil {
		return err
	}
	return derr
}
