package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

// Test that in-code migration results in the same schema as the .sql one.
func Test0021Migration_CompatibleSchema(t *testing.T) {
	db := sql.InMemory(
		sql.WithLogger(zaptest.NewLogger(t)),
		sql.WithMigration(New0021Migration(zaptest.NewLogger(t), 1000)),
	)

	var schemasInCode []string
	_, err := db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		schemasInCode = append(schemasInCode, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db = sql.InMemory()

	var schemasInFile []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		schemasInFile = append(schemasInFile, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.Equal(t, schemasInFile, schemasInCode)
}

func Test0021Migration(t *testing.T) {
	db := sql.InMemory(
		sql.WithLogger(zaptest.NewLogger(t)),
		sql.WithSkipMigrations(21),
	)

	var signers [177]*signing.EdSigner
	for i := range signers {
		var err error
		signers[i], err = signing.NewEdSigner()
		require.NoError(t, err)
	}
	type post struct {
		id    types.NodeID
		units uint32
	}
	allPosts := make(map[types.EpochID]map[types.ATXID]post)
	for epoch := range types.EpochID(40) {
		allPosts[epoch] = make(map[types.ATXID]post)
		for _, signer := range signers {
			watx := wire.ActivationTxV1{
				InnerActivationTxV1: wire.InnerActivationTxV1{
					NumUnits: epoch.Uint32() * 10,
					Coinbase: types.Address(types.RandomBytes(24)),
				},
				SmesherID: signer.NodeID(),
			}
			require.NoError(t, atxs.AddBlob(db, watx.ID(), codec.MustEncode(&watx), 0))
			allPosts[epoch][watx.ID()] = post{
				id:    signer.NodeID(),
				units: watx.NumUnits,
			}
		}
	}

	m := New0021Migration(zaptest.NewLogger(t), 1000)
	require.Equal(t, 21, m.Order())
	require.NoError(t, m.Apply(db))

	for _, posts := range allPosts {
		for atx, post := range posts {
			units, err := atxs.Units(db, atx, post.id)
			require.NoError(t, err)
			require.Equal(t, post.units, units)
		}
	}
}
