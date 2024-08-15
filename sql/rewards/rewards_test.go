package rewards

import (
	"math"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestRewards(t *testing.T) {
	db := statesql.InMemory()

	var part uint64 = math.MaxUint64 / 2
	lyrReward := part / 2
	coinbase1 := types.Address{1}
	coinbase2 := types.Address{2}
	smesherID1 := types.NodeID{1}
	smesherID2 := types.NodeID{2}
	smesherID3 := types.NodeID{3}

	lid1 := types.LayerID(1)
	rewards1 := []types.Reward{
		{
			Layer:       lid1,
			Coinbase:    coinbase1,
			SmesherID:   smesherID1,
			TotalReward: part,
			LayerReward: lyrReward,
		},
		{
			Layer:       lid1,
			Coinbase:    coinbase1,
			SmesherID:   smesherID2,
			TotalReward: part,
			LayerReward: lyrReward,
		},
		{
			Layer:       lid1,
			Coinbase:    coinbase2,
			SmesherID:   smesherID3,
			TotalReward: part,
			LayerReward: lyrReward,
		},
	}
	lid2 := lid1.Add(1)
	rewards2 := []types.Reward{
		{
			Layer:       lid2,
			Coinbase:    coinbase2,
			SmesherID:   smesherID2,
			TotalReward: part,
			LayerReward: lyrReward,
		},
	}
	for _, reward := range append(rewards1, rewards2...) {
		require.NoError(t, Add(db, &reward))
	}

	got, err := ListByCoinbase(db, coinbase1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase1, got[1].Coinbase)
	require.Equal(t, lid1, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListByCoinbase(db, coinbase2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase2, got[1].Coinbase)
	require.Equal(t, lid2, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListBySmesherId(db, smesherID1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID2, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase2, got[1].Coinbase)
	require.Equal(t, lid2, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListBySmesherId(db, smesherID3)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	unknownAddr := types.Address{1, 2, 3}
	got, err = ListByCoinbase(db, unknownAddr)
	require.NoError(t, err)
	require.Empty(t, got)

	unknownSmesher := types.NodeID{1, 2, 3}
	got, err = ListBySmesherId(db, unknownSmesher)
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, Revert(db, lid1))
	got, err = ListByCoinbase(db, coinbase1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase1, got[1].Coinbase)
	require.Equal(t, lid1, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListByCoinbase(db, coinbase2)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID2)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID2, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID3)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	// This should fail: there cannot be two (smesherID, layer) rows.
	require.ErrorIs(t, Add(db, &types.Reward{
		Layer:     lid1,
		SmesherID: smesherID2,
	}), sql.ErrObjectExists)

	// This should succeed. SmesherID can be NULL.
	require.NoError(t, Add(db, &types.Reward{
		Layer: lid1,
	}))

	// This should fail: there cannot be two (NULL, layer) rows.
	require.ErrorIs(t, Add(db, &types.Reward{
		Layer: lid1,
	}), sql.ErrObjectExists)
}

func Test_0008Migration_EmptyDBIsNoOp(t *testing.T) {
	schema, err := statesql.Schema()
	require.NoError(t, err)
	sort.Slice(schema.Migrations, func(i, j int) bool {
		return schema.Migrations[i].Order() < schema.Migrations[j].Order()
	})
	origMigrations := schema.Migrations
	schema.Migrations = schema.Migrations[:7]

	// apply previous migrations
	db := statesql.InMemory(
		sql.WithDatabaseSchema(schema),
	)

	// verify that the DB is empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *sql.Statement) {
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// apply the migration
	err = origMigrations[7].Apply(db, zaptest.NewLogger(t))
	require.NoError(t, err)

	// verify that db is still empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *sql.Statement) {
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)
}

func Test_0008Migration(t *testing.T) {
	schema, err := statesql.Schema()
	require.NoError(t, err)
	sort.Slice(schema.Migrations, func(i, j int) bool {
		return schema.Migrations[i].Order() < schema.Migrations[j].Order()
	})
	origMigrations := schema.Migrations
	schema.Migrations = schema.Migrations[:7]

	// apply previous migrations
	db := statesql.InMemory(
		sql.WithDatabaseSchema(schema),
		sql.WithForceMigrations(true),
		sql.WithAllowSchemaDrift(true),
	)

	// verify that the DB is empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *sql.Statement) {
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// attempt to insert some rewards data
	reward := &types.Reward{
		Layer:       9000,
		TotalReward: 10,
		LayerReward: 20,
		Coinbase:    types.Address{1},
	}
	err = Add(db, reward)

	// this should fail since the un-migrated table doesn't have this column yet
	require.ErrorContains(t, err, "table rewards has no column named pubkey")

	// add the row manually
	_, err = db.Exec(`
		insert into rewards (coinbase, layer, total_reward, layer_reward) values (?1, ?2, ?3, ?4)`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, reward.Coinbase[:])
			stmt.BindInt64(2, int64(reward.Layer.Uint32()))
			stmt.BindInt64(3, int64(reward.TotalReward))
			stmt.BindInt64(4, int64(reward.LayerReward))
		}, nil)
	require.NoError(t, err)

	// make sure one row was added successfully
	_, err = db.Exec("select count(*) from rewards;", func(stmt *sql.Statement) {
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(1), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// apply the migration
	err = origMigrations[7].Apply(db, zaptest.NewLogger(t))
	require.NoError(t, err)

	// verify that one row is still present
	_, err = db.Exec("select count(*) from rewards;", func(stmt *sql.Statement) {
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(1), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// verify the data
	rewards, err := ListByCoinbase(db, reward.Coinbase)
	require.NoError(t, err)
	require.Len(t, rewards, 1)
	require.Equal(t, reward.Coinbase, rewards[0].Coinbase)
	require.Equal(t, reward.TotalReward, rewards[0].TotalReward)
	require.Equal(t, reward.LayerReward, rewards[0].LayerReward)
	require.Equal(t, reward.Layer, rewards[0].Layer)
	// this should not be set
	require.Equal(t, types.NodeID{0}, rewards[0].SmesherID)

	// this should return nothing (since smesherID wasn't set)
	rewards, err = ListBySmesherId(db, reward.SmesherID)
	require.NoError(t, err)
	require.Empty(t, rewards)

	// add more data and verify that we can read it both ways
	reward = &types.Reward{
		Layer:       9001,
		TotalReward: 11,
		LayerReward: 21,
		Coinbase:    types.Address{1},
		SmesherID:   types.NodeID{2},
	}

	err = Add(db, reward)
	require.NoError(t, err)
	rewards, err = ListByCoinbase(db, reward.Coinbase)
	require.NoError(t, err)
	require.Len(t, rewards, 2)
	require.Equal(t, reward.Coinbase, rewards[1].Coinbase)
	require.Equal(t, reward.TotalReward, rewards[1].TotalReward)
	require.Equal(t, reward.LayerReward, rewards[1].LayerReward)
	require.Equal(t, reward.Layer, rewards[1].Layer)
	require.Equal(t, reward.SmesherID, rewards[1].SmesherID)

	rewards, err = ListBySmesherId(db, reward.SmesherID)
	require.NoError(t, err)
	require.Len(t, rewards, 1)
	require.Equal(t, reward.Coinbase, rewards[0].Coinbase)
	require.Equal(t, reward.TotalReward, rewards[0].TotalReward)
	require.Equal(t, reward.LayerReward, rewards[0].LayerReward)
	require.Equal(t, reward.Layer, rewards[0].Layer)
	require.Equal(t, reward.SmesherID, rewards[0].SmesherID)
}
