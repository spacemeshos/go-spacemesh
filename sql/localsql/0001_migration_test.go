package localsql

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Test_0001Migration_CompatibleSQL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test1.db")
	db, err := Open("file:"+file,
		sql.WithMigration(New0001Migration(t.TempDir())),
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

func Test_0001Migration_AddsMissingData_Post(t *testing.T) {
	dataDir := t.TempDir()

	post := &types.Post{
		Nonce:   1,
		Indices: []byte{1, 2, 3},
		Pow:     1,
	}
	require.NoError(t, savePost(dataDir, post))
	require.FileExists(t, filepath.Join(dataDir, postFilename))

	nodeID := types.RandomNodeID()
	refCommitmentATX := types.RandomATXID()
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:          nodeID.Bytes(),
		CommitmentAtxId: refCommitmentATX.Bytes(),
	})
	require.NoError(t, err)

	db := InMemory(
		sql.WithMigrations(nil),
		sql.WithMigration(New0001Migration(dataDir)),
	)

	_, err = db.Exec(
		`select post_nonce, post_indices, post_pow, commit_atx
		from initial_post where id = ?1 limit 1;`, func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, func(stmt *sql.Statement) bool {
			require.Equal(t, post.Nonce, uint32(stmt.ColumnInt64(0)))

			buf := make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, buf)
			require.Equal(t, post.Indices, buf)

			require.Equal(t, post.Pow, uint64(stmt.ColumnInt64(2)))

			var commitmentATX types.ATXID
			stmt.ColumnBytes(3, commitmentATX[:])
			require.Equal(t, refCommitmentATX, commitmentATX)
			return true
		})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, postFilename))
}

func Test_0001Migration_AddsMissingData_NIPostChallenge(t *testing.T) {
	dataDir := t.TempDir()

	refChallenge := &types.NIPostChallenge{
		PublishEpoch:   4,
		Sequence:       1,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  nil,
		InitialPost:    nil,
	}
	require.NoError(t, saveNipostChallenge(dataDir, refChallenge))
	require.FileExists(t, filepath.Join(dataDir, challengeFilename))

	nodeID := types.RandomNodeID()
	commitmentATX := types.RandomATXID()
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:          nodeID.Bytes(),
		CommitmentAtxId: commitmentATX.Bytes(),
	})
	require.NoError(t, err)

	db := InMemory(
		sql.WithMigrations(nil),
		sql.WithMigration(New0001Migration(dataDir)),
	)

	_, err = db.Exec(`
		select epoch, sequence, prev_atx, pos_atx, commit_atx
		from nipost where id = ?1 limit 1;`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, refChallenge.PublishEpoch, types.EpochID(stmt.ColumnInt64(0)))
		require.Equal(t, refChallenge.Sequence, uint64(stmt.ColumnInt64(1)))

		var prevATX types.ATXID
		stmt.ColumnBytes(2, prevATX[:])
		require.Equal(t, refChallenge.PrevATXID, prevATX)

		var posATX types.ATXID
		stmt.ColumnBytes(3, posATX[:])
		require.Equal(t, refChallenge.PositioningATX, posATX)
		return true
	})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, challengeFilename))
}

func Test_0001Migration_AddsMissingData_NIPostChallenge_InitialPost(t *testing.T) {
	dataDir := t.TempDir()

	commitmentATX := types.RandomATXID()
	refChallenge := &types.NIPostChallenge{
		PublishEpoch:   4,
		Sequence:       0,
		PrevATXID:      types.EmptyATXID,
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  &commitmentATX,
		InitialPost: &types.Post{
			Nonce:   1,
			Indices: []byte{1, 2, 3},
			Pow:     1,
		},
	}
	require.NoError(t, saveNipostChallenge(dataDir, refChallenge))
	require.FileExists(t, filepath.Join(dataDir, challengeFilename))

	nodeID := types.RandomNodeID()

	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:          nodeID.Bytes(),
		CommitmentAtxId: commitmentATX.Bytes(),
	})
	require.NoError(t, err)

	db := InMemory(
		sql.WithMigrations(nil),
		sql.WithMigration(New0001Migration(dataDir)),
	)

	_, err = db.Exec(`
		select epoch, sequence, prev_atx, pos_atx, commit_atx, post_nonce, post_indices, post_pow
		from nipost where id = ?1 limit 1;`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, refChallenge.PublishEpoch, types.EpochID(stmt.ColumnInt64(0)))
		require.Equal(t, refChallenge.Sequence, uint64(stmt.ColumnInt64(1)))

		var prevATX types.ATXID
		stmt.ColumnBytes(2, prevATX[:])
		require.Equal(t, refChallenge.PrevATXID, prevATX)

		var posATX types.ATXID
		stmt.ColumnBytes(3, posATX[:])
		require.Equal(t, refChallenge.PositioningATX, posATX)

		var commitATX types.ATXID
		stmt.ColumnBytes(4, commitATX[:])
		require.Equal(t, *refChallenge.CommitmentATX, commitATX)

		require.Equal(t, refChallenge.InitialPost.Nonce, uint32(stmt.ColumnInt64(5)))

		buf := make([]byte, stmt.ColumnLen(6))
		stmt.ColumnBytes(6, buf)
		require.Equal(t, refChallenge.InitialPost.Indices, buf)

		require.Equal(t, refChallenge.InitialPost.Pow, uint64(stmt.ColumnInt64(7)))
		return true
	})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, challengeFilename))
}
