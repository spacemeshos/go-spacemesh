package marriage_test

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestFind(t *testing.T) {
	t.Parallel()
	db := statesql.InMemoryTest(t)

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	info := marriage.Info{
		ID:            id1,
		NodeID:        types.RandomNodeID(),
		ATX:           types.RandomATXID(),
		MarriageIndex: rand.N(256),
		Target:        types.RandomNodeID(),
		Signature:     types.RandomEdSignature(),
	}
	err = marriage.Add(db, info)
	require.NoError(t, err)

	id, err := marriage.FindIDByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, id1, id)
	info.ID = id1

	res, err := marriage.FindByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, info, res)

	_, err = marriage.FindIDByNodeID(db, types.RandomNodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	_, err = marriage.FindByNodeID(db, types.RandomNodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAdd(t *testing.T) {
	t.Parallel()
	db := statesql.InMemoryTest(t)

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	info := marriage.Info{
		ID:            id1,
		NodeID:        types.RandomNodeID(),
		ATX:           types.RandomATXID(),
		MarriageIndex: rand.N(256),
		Target:        types.RandomNodeID(),
		Signature:     types.RandomEdSignature(),
	}
	err = marriage.Add(db, info)
	require.NoError(t, err)

	id, err := marriage.FindIDByNodeID(db, info.NodeID)
	require.NoError(t, err)
	require.Equal(t, id1, id)
	info.ID = id1

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
	t.Parallel()

	t.Run("update marriage ATX and index", func(t *testing.T) {
		// this updates the existing entry (e.g. when `nodeID` married a second time)
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: rand.N(256),
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}

		err = marriage.Add(db, info)
		require.NoError(t, err)

		id2, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id2)
		require.NotEqual(t, id1, id2)

		info = marriage.Info{
			ID:            id1,
			NodeID:        info.NodeID,
			ATX:           types.RandomATXID(),
			MarriageIndex: rand.N(256),
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		res, err := marriage.FindByNodeID(db, info.NodeID)
		require.NoError(t, err)
		require.Equal(t, info, res)
	})

	t.Run("update marriage id", func(t *testing.T) {
		// this would update the marriage ID should fail (needs to be done explicitly with marriage.UpdateID)
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: rand.N(256),
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}

		err = marriage.Add(db, info)
		require.NoError(t, err)

		id2, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id2)
		require.NotEqual(t, id1, id2)

		info.ID = id2
		err = marriage.Add(db, info)
		require.ErrorIs(t, err, sql.ErrConflict)
		require.ErrorContains(t, err, info.NodeID.String())
	})
}

func TestAddUniqueConstraints(t *testing.T) {
	t.Parallel()

	t.Run("same nodeID can't be married twice", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: 1,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		id2, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)
		require.NotEqual(t, id1, id2)

		info.ID = id2
		err = marriage.Add(db, info)
		require.ErrorIs(t, err, sql.ErrConflict)
	})

	t.Run("different nodeID, same marriage ATX, different index", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: 1,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		info.ID = id1
		info.NodeID = types.RandomNodeID()
		info.MarriageIndex = 2
		err = marriage.Add(db, info)
		require.NoError(t, err)
	})

	t.Run("different nodeID, different marriage ATX, same index", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: 1,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		info.NodeID = types.RandomNodeID()
		info.ATX = types.RandomATXID()
		info.MarriageIndex = 2
		err = marriage.Add(db, info)
		require.NoError(t, err)
	})

	t.Run("different nodeID, same marriage ATX, same index", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: 1,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		// different nodeID, same marriageATX with same index should fail
		info.NodeID = types.RandomNodeID()
		err = marriage.Add(db, info)
		require.ErrorIs(t, err, sql.ErrObjectExists)
	})
}

func TestNodeIDsByID(t *testing.T) {
	t.Parallel()
	t.Run("invalid ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		ids, err := marriage.NodeIDsByID(db, 101)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Empty(t, ids)
	})

	t.Run("valid ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nodeIDs := make([]types.NodeID, 3)
		info := marriage.Info{
			ATX:       types.RandomATXID(),
			Target:    types.RandomNodeID(),
			Signature: types.RandomEdSignature(),
		}

		id1, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id1)

		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
			err = marriage.Add(db, marriage.Info{
				ID:            id1,
				NodeID:        nodeIDs[i],
				ATX:           info.ATX,
				MarriageIndex: i,
				Target:        info.Target,
				Signature:     info.Signature,
			})
			require.NoError(t, err)
		}

		res, err := marriage.NodeIDsByID(db, id1)
		require.NoError(t, err)
		require.ElementsMatch(t, nodeIDs, res)

		// NewID should return a different ID
		id2, err := marriage.NewID(db)
		require.NoError(t, err)
		require.NotZero(t, id2)
		require.NotEqual(t, id1, id2)

		nodeIDs, err = marriage.NodeIDsByID(db, id2)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Empty(t, nodeIDs)
	})
}

func TestIterateOps(t *testing.T) {
	t.Parallel()
	db := statesql.InMemoryTest(t)

	id1, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id1)

	// add 2 sets of 5 identities each
	infos := make([]marriage.Info, 0, 10)
	for i := range 5 {
		info := marriage.Info{
			ID:            id1,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: i,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		infos = append(infos, info)
	}

	id2, err := marriage.NewID(db)
	require.NoError(t, err)
	require.NotZero(t, id2)
	require.NotEqual(t, id1, id2)

	for range 5 {
		info := marriage.Info{
			ID:            id2,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: rand.N(256),
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		}
		err = marriage.Add(db, info)
		require.NoError(t, err)

		infos = append(infos, info)
	}

	// iterate all 10
	res := make([]marriage.Info, 0, 10)
	marriage.IterateOps(db, builder.Operations{}, func(info marriage.Info) bool {
		res = append(res, info)
		return true
	})
	require.ElementsMatch(t, infos, res)

	// iterate only the first marriage
	res = make([]marriage.Info, 0, 5)
	operations := builder.Operations{
		Filter: []builder.Op{
			{Field: builder.Id, Token: builder.Eq, Value: int64(id1)},
		},
	}
	marriage.IterateOps(db, operations, func(info marriage.Info) bool {
		res = append(res, info)
		return true
	})
	require.ElementsMatch(t, infos[:5], res)
}
