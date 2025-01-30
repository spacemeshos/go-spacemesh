package localatxs_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/localatxs"
)

func Test_Blobs(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		db := localsql.InMemoryTest(t)
		_, _, _, err := localatxs.AtxAndPoet(db, types.EpochID(0), types.NodeID{})
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
	t.Run("found", func(t *testing.T) {
		db := localsql.InMemoryTest(t)
		epoch := types.EpochID(2)
		atxid := types.RandomATXID()
		nodeID := types.RandomNodeID()
		blob := types.RandomBytes(10)
		poet := types.PoetProofRef(types.RandomHash())
		err := localatxs.AddAtx(db, epoch, atxid, nodeID, blob, poet)
		require.NoError(t, err)
		gotID, gotBlob, gotPoet, err := localatxs.AtxAndPoet(db, epoch, nodeID)
		require.NoError(t, err)
		require.Equal(t, atxid, gotID)
		require.Equal(t, blob, gotBlob)
		require.Equal(t, poet, gotPoet)

		// different ID
		_, _, _, err = localatxs.AtxAndPoet(db, epoch, types.RandomNodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)

		// different epoch
		_, _, _, err = localatxs.AtxAndPoet(db, types.EpochID(3), nodeID)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
}
