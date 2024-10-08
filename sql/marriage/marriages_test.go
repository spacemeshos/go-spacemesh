package marriage_test

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestFindByNodeID(t *testing.T) {
	db := statesql.InMemoryTest(t)

	info := marriage.Info{
		NodeID:         types.RandomNodeID(),
		MarriageATX:    types.RandomATXID(),
		MarriageIndex:  rand.N(256),
		MarriageTarget: types.RandomNodeID(),
		MarriageSig:    types.RandomEdSignature(),
	}

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	err = marriage.Add(db, id1, info)
	require.NoError(t, err)

	id, err := marriage.FindIDByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, id1, id)

	res, err := marriage.FindByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, info, res)

	_, err = marriage.FindIDByNodeID(db, types.RandomNodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	_, err = marriage.FindByNodeID(db, types.RandomNodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAdd(t *testing.T) {
	db := statesql.InMemoryTest(t)

	info := marriage.Info{
		NodeID:         types.RandomNodeID(),
		MarriageATX:    types.RandomATXID(),
		MarriageIndex:  rand.N(256),
		MarriageTarget: types.RandomNodeID(),
		MarriageSig:    types.RandomEdSignature(),
	}

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	err = marriage.Add(db, id1, info)
	require.NoError(t, err)

	id, err := marriage.FindIDByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, id1, id)

	res, err := marriage.FindByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, info, res)

	// NewID should return a different ID
	id2, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id2)
	require.NotEqual(t, id1, id2)
}

func TestAddUpdatesExisting(t *testing.T) {
	db := statesql.InMemoryTest(t)

	info := marriage.Info{
		NodeID:         types.RandomNodeID(),
		MarriageATX:    types.RandomATXID(),
		MarriageIndex:  rand.N(256),
		MarriageTarget: types.RandomNodeID(),
		MarriageSig:    types.RandomEdSignature(),
	}

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	err = marriage.Add(db, id1, info)
	require.NoError(t, err)

	id2, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id2)
	require.NotEqual(t, id1, id2)

	info = marriage.Info{
		NodeID:         info.NodeID,
		MarriageATX:    types.RandomATXID(),
		MarriageIndex:  rand.N(256),
		MarriageTarget: types.RandomNodeID(),
		MarriageSig:    types.RandomEdSignature(),
	}

	// this updates the existing entry (e.g. when `nodeID` married a second time)
	err = marriage.Add(db, id1, info)
	require.NoError(t, err)

	res, err := marriage.FindByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, info, res)

	// this would assign nodeID to a different marriage set and should fail
	err = marriage.Add(db, id2, info)
	require.ErrorIs(t, err, sql.ErrConflict)
	require.ErrorContains(t, err, info.NodeID.String())
}

func TestAddUniqueConstraints(t *testing.T) {
	db := statesql.InMemoryTest(t)

	info := marriage.Info{
		NodeID:         types.RandomNodeID(),
		MarriageATX:    types.RandomATXID(),
		MarriageIndex:  1,
		MarriageTarget: types.RandomNodeID(),
		MarriageSig:    types.RandomEdSignature(),
	}

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	err = marriage.Add(db, id1, info)
	require.NoError(t, err)

	// same node can't be married twice
	id2, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)
	require.NotEqual(t, id1, id2)

	err = marriage.Add(db, id2, info)
	require.ErrorIs(t, err, sql.ErrConflict)

	// different nodeID, same marriageATX with different index should be allowed
	info.NodeID = types.RandomNodeID()
	info.MarriageIndex = 2
	err = marriage.Add(db, id1, info)
	require.NoError(t, err)

	// different nodeID, same marriageATX with same index should fail
	info.NodeID = types.RandomNodeID()
	err = marriage.Add(db, id1, info)
	require.ErrorIs(t, err, sql.ErrObjectExists)

	// different nodeID, different marriageATX with same index should be allowed
	info.NodeID = types.RandomNodeID()
	info.MarriageATX = types.RandomATXID()
	info.MarriageIndex = 2
	err = marriage.Add(db, id1, info)
	require.NoError(t, err)
}

func TestNodeIDsByID(t *testing.T) {
	db := statesql.InMemoryTest(t)

	nodeIDs := make([]types.NodeID, 3)
	info := marriage.Info{
		MarriageATX:    types.RandomATXID(),
		MarriageTarget: types.RandomNodeID(),
		MarriageSig:    types.RandomEdSignature(),
	}

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	for i := range nodeIDs {
		nodeIDs[i] = types.RandomNodeID()
		err = marriage.Add(db, id1, marriage.Info{
			NodeID:         nodeIDs[i],
			MarriageATX:    info.MarriageATX,
			MarriageIndex:  i,
			MarriageTarget: info.MarriageTarget,
			MarriageSig:    info.MarriageSig,
		})
		require.NoError(t, err)
	}

	res, err := marriage.NodeIDsByID(db, id1)
	require.NoError(t, err)
	require.Equal(t, nodeIDs, res)

	// NewID should return a different ID
	id2, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id2)
	require.NotEqual(t, id1, id2)

	nodeIDs, err = marriage.NodeIDsByID(db, id2)
	require.NoError(t, err)
	require.Empty(t, nodeIDs)
}
