package migrations

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test0021Migration(t *testing.T) {
	schema, err := statesql.Schema()
	require.NoError(t, err)
	schema.Migrations = slices.DeleteFunc(schema.Migrations, func(m sql.Migration) bool {
		return m.Order() == 21
	})

	db := sql.InMemory(
		sql.WithLogger(zaptest.NewLogger(t)),
		sql.WithDatabaseSchema(schema),
		sql.WithNoCheckSchemaDrift(),
		sql.WithForceMigrations(true),
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

	m := New0021Migration(1000)
	require.Equal(t, 21, m.Order())
	require.NoError(t, m.Apply(db, zaptest.NewLogger(t)))

	for _, posts := range allPosts {
		for atx, post := range posts {
			units, err := atxs.Units(db, atx, post.id)
			require.NoError(t, err)
			require.Equal(t, post.units, units)
		}
	}
}
