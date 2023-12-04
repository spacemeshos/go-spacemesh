package localsql

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Test_0002Migration_CompatibleSQL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test1.db")
	db, err := Open("file:"+file,
		sql.WithMigration(New0002Migration(t.TempDir())),
	)
	require.NoError(t, err)
	var sqls1 []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		sqls1 = append(sqls1, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	file = filepath.Join(t.TempDir(), "test2.db")
	db, err = Open("file:" + file)
	require.NoError(t, err)
	var sqls2 []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		sqls2 = append(sqls2, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.Equal(t, sqls1, sqls2)
}

func Test_0002Migration_AddsMissingData(t *testing.T) {
	// apply only the first migration
	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:1]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	nodeID := types.RandomNodeID()
	commitmentATX := types.RandomATXID()
	numUnits := uint32(8)
	post := &types.Post{
		Nonce:   1,
		Indices: []byte{1, 2, 3},
		Pow:     4,
	}
	vrfNonce := uint64(5)

	dir := t.TempDir()
	err = initialization.SaveMetadata(dir, &shared.PostMetadata{
		NodeId:          nodeID.Bytes(),
		CommitmentAtxId: commitmentATX.Bytes(),

		LabelsPerUnit: 1024,
		NumUnits:      numUnits,
		Nonce:         &vrfNonce,
	})
	require.NoError(t, err)

	// insert initial post with missing commit_atx, vrf_nonce and num_units
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(post.Nonce))
		stmt.BindBytes(3, post.Indices)
		stmt.BindInt64(4, int64(post.Pow))
	}
	_, err = db.Exec(`
		insert into initial_post (
			id, post_nonce, post_indices, post_pow
		) values (?1, ?2, ?3, ?4);`, enc, nil,
	)
	require.NoError(t, err)

	// apply migration
	migration := New0002Migration(dir)
	err = migration.Apply(db)
	require.NoError(t, err)

	// verify that the missing fields were populated and original fields were not changed
	_, err = db.Exec(`
		select post_nonce, post_indices, post_pow, num_units, commit_atx, vrf_nonce
		from initial_post
		where id = ?1;`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(post.Nonce), stmt.ColumnInt64(0))

		buf := make([]byte, stmt.ColumnLen(1))
		stmt.ColumnBytes(1, buf)
		require.Equal(t, post.Indices, buf)

		require.Equal(t, int64(post.Pow), stmt.ColumnInt64(2))

		require.Equal(t, int64(numUnits), stmt.ColumnInt64(3))

		buf = make([]byte, stmt.ColumnLen(4))
		stmt.ColumnBytes(4, buf)
		require.Equal(t, commitmentATX.Bytes(), buf)

		require.Equal(t, vrfNonce, uint64(stmt.ColumnInt64(5)))
		return true
	})
	require.NoError(t, err)
}

func Test_0002Migration_EmptyDBIsNoOp(t *testing.T) {
	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)

	// apply only the first migration
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:1]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	dir := t.TempDir()

	// apply migration
	migration := New0002Migration(dir)
	err = migration.Apply(db)
	require.NoError(t, err)

	// verify that db is still empty
	_, err = db.Exec("select count(*) from initial_post where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, types.RandomNodeID().Bytes())
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, int64(0), stmt.ColumnInt64(0))
		return true
	})
	require.NoError(t, err)
}
