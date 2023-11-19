package sql

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationsAppliedOnce(t *testing.T) {
	db := InMemory()

	var version int
	_, err := db.Exec("PRAGMA user_version;", nil, func(stmt *Statement) bool {
		version = stmt.ColumnInt(0)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, version, 8)
}

func Test_0008Migration_EmptyDBIsNoOp(t *testing.T) {
	migrations, err := StateMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })

	// apply previous migrations
	db := InMemory(
		WithMigrations(migrations[:7]),
	)

	// verify that the DB is empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *Statement) {
	}, func(stmt *Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// apply the migration
	err = migrations[7].Apply(db)
	require.NoError(t, err)

	// verify that db is still empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *Statement) {
	}, func(stmt *Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)
}

func Test_0008Migration(t *testing.T) {
	migrations, err := StateMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })

	// apply previous migrations
	db := InMemory(
		WithMigrations(migrations[:7]),
	)

	// verify that the DB is empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *Statement) {
	}, func(stmt *Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// insert some rewards data in the old format
	err = rewards.Add(db, &types.Reward{
		Layer:       9000,
		TotalReward: 10,
		LayerReward: 20,
		Coinbase:    types.Address{1},
	})
	require.NoError(t, err)
	err = rewards.Add(db, &types.Reward{
		Layer:       9000,
		TotalReward: 10,
		LayerReward: 20,
		Coinbase:    types.Address{1},
		SmesherID:   types.NodeID{2},
	})
	require.NoError(t, err)

	// verify the table format
	_, err = db.Exec("select count(*) from rewards;", func(stmt *Statement) {
	}, func(stmt *Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)

	// apply the migration
	err = migrations[7].Apply(db)
	require.NoError(t, err)

	// verify that db is still empty
	_, err = db.Exec("select count(*) from rewards;", func(stmt *Statement) {
	}, func(stmt *Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)
}
