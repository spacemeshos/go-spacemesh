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
		_, _, err := localatxs.AtxBlob(db, types.EpochID(0), types.NodeID{})
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
	t.Run("found", func(t *testing.T) {
		db := localsql.InMemoryTest(t)
		epoch := types.EpochID(2)
		atxid := types.RandomATXID()
		nodeID := types.RandomNodeID()
		blob := types.RandomBytes(10)
		err := localatxs.AddBlob(db, epoch, atxid, nodeID, blob)
		require.NoError(t, err)
		gotID, gotBlob, err := localatxs.AtxBlob(db, epoch, nodeID)
		require.NoError(t, err)
		require.Equal(t, atxid, gotID)
		require.Equal(t, blob, gotBlob)

		// different ID
		_, _, err = localatxs.AtxBlob(db, epoch, types.RandomNodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)

		// different epoch
		_, _, err = localatxs.AtxBlob(db, types.EpochID(3), nodeID)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
}
