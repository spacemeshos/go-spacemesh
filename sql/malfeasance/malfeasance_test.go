package malfeasance_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestAdd(t *testing.T) {
	t.Parallel()

	t.Run("without proof and marriage ID fails", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		err := malfeasance.AddProof(db, types.RandomNodeID(), nil, nil, 1, time.Now())
		require.Error(t, err)
	})

	t.Run("with proof and without marriage ID succeeds", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		err := malfeasance.AddProof(db, types.RandomNodeID(), nil, types.RandomBytes(100), 1, time.Now())
		require.NoError(t, err)
	})

	t.Run("without proof and with unknown marriage ID fails", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id := marriage.ID(100)
		err := malfeasance.AddProof(db, types.RandomNodeID(), &id, nil, 1, time.Now())
		require.Error(t, err)
	})

	t.Run("without proof and with known marriage ID succeeds", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id, err := marriage.NewID(db)
		require.NoError(t, err)

		nodeID := types.RandomNodeID()
		err = marriage.Add(db, marriage.Info{
			ID:            id,
			NodeID:        nodeID,
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		err = malfeasance.AddProof(db, types.RandomNodeID(), &id, nil, 1, time.Now())
		require.NoError(t, err)
	})

	t.Run("with proof and with known marriage ID succeeds", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id, err := marriage.NewID(db)
		require.NoError(t, err)

		nodeID := types.RandomNodeID()
		err = marriage.Add(db, marriage.Info{
			ID:            id,
			NodeID:        nodeID,
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		err = malfeasance.AddProof(db, types.RandomNodeID(), &id, types.RandomBytes(100), 1, time.Now())
		require.NoError(t, err)
	})
}

func TestSetMalicious(t *testing.T) {
	t.Parallel()

	t.Run("identity cannot be set malicious with unknown marriage ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nodeID := types.RandomNodeID()
		err := malfeasance.SetMalicious(db, nodeID, marriage.ID(0), time.Now())
		require.Error(t, err)

		mal, err := malfeasance.IsMalicious(db, nodeID)
		require.NoError(t, err)
		require.False(t, mal)
	})

	t.Run("identity can be set malicious with known marriage ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id, err := marriage.NewID(db)
		require.NoError(t, err)

		nodeID := types.RandomNodeID()
		err = marriage.Add(db, marriage.Info{
			ID:            id,
			NodeID:        nodeID,
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		err = malfeasance.SetMalicious(db, nodeID, id, time.Now())
		require.NoError(t, err)

		mal, err := malfeasance.IsMalicious(db, nodeID)
		require.NoError(t, err)
		require.True(t, mal)
	})

	t.Run("malfeasants marriage ID cannot be updated with SetMalicious to unknown ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nodeID := types.RandomNodeID()
		err := malfeasance.AddProof(db, nodeID, nil, types.RandomBytes(100), 1, time.Now())
		require.NoError(t, err)

		err = malfeasance.SetMalicious(db, nodeID, marriage.ID(0), time.Now())
		require.Error(t, err)

		mal, err := malfeasance.IsMalicious(db, nodeID)
		require.NoError(t, err)
		require.True(t, mal)
	})

	t.Run("malfeasants marriage ID can be updated with SetMalicious to known ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id, err := marriage.NewID(db)
		require.NoError(t, err)

		nodeID := types.RandomNodeID()
		err = malfeasance.AddProof(db, nodeID, nil, types.RandomBytes(100), 1, time.Now())
		require.NoError(t, err)

		err = marriage.Add(db, marriage.Info{
			ID:            id,
			NodeID:        types.RandomNodeID(),
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		err = malfeasance.SetMalicious(db, nodeID, id, time.Now())
		require.NoError(t, err)

		mal, err := malfeasance.IsMalicious(db, nodeID)
		require.NoError(t, err)
		require.True(t, mal)
	})
}

func TestIsMalicious(t *testing.T) {
	t.Parallel()

	t.Run("unknown node is not malicious", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		mal, err := malfeasance.IsMalicious(db, types.RandomNodeID())
		require.NoError(t, err)
		require.False(t, mal)
	})

	t.Run("known node is malicious", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nodeID := types.RandomNodeID()
		err := malfeasance.AddProof(db, nodeID, nil, types.RandomBytes(100), 1, time.Now())
		require.NoError(t, err)

		mal, err := malfeasance.IsMalicious(db, nodeID)
		require.NoError(t, err)
		require.True(t, mal)
	})
}

func Test_IterateMaliciousOps(t *testing.T) {
	db := statesql.InMemoryTest(t)
	tt := []struct {
		id     types.NodeID
		proof  []byte
		domain int
	}{
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
			rand.IntN(255),
		},
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
			rand.IntN(255),
		},
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
			rand.IntN(255),
		},
	}

	for _, tc := range tt {
		err := malfeasance.AddProof(db, tc.id, nil, tc.proof, tc.domain, time.Now())
		require.NoError(t, err)
	}

	var got []struct {
		id     types.NodeID
		proof  []byte
		domain int
	}
	err := malfeasance.IterateOps(db, builder.Operations{},
		func(id types.NodeID, proof []byte, domain int, _ time.Time) bool {
			got = append(got, struct {
				id     types.NodeID
				proof  []byte
				domain int
			}{id, proof, domain})
			return true
		})
	require.NoError(t, err)
	require.ElementsMatch(t, tt, got)
}

func Test_IterateMaliciousOps_Married(t *testing.T) {
	db := statesql.InMemoryTest(t)

	nodeID := types.RandomNodeID()
	marriageATX := types.RandomATXID()
	id, err := marriage.NewID(db)
	require.NoError(t, err)

	err = marriage.Add(db, marriage.Info{
		ID:            id,
		NodeID:        nodeID,
		ATX:           marriageATX,
		MarriageIndex: 0,
		Target:        nodeID,
		Signature:     types.RandomEdSignature(),
	})
	require.NoError(t, err)

	ids := make([]types.NodeID, 5)
	ids[0] = nodeID
	proof := types.RandomBytes(11)
	err = malfeasance.AddProof(db, ids[0], &id, proof, 1, time.Now())
	require.NoError(t, err)

	for i := 1; i < len(ids); i++ {
		ids[i] = types.RandomNodeID()
		err := malfeasance.SetMalicious(db, ids[i], id, time.Now())
		require.NoError(t, err)
	}

	var got []struct {
		id     types.NodeID
		proof  []byte
		domain int
	}
	err = malfeasance.IterateOps(db, builder.Operations{},
		func(id types.NodeID, proof []byte, domain int, _ time.Time) bool {
			got = append(got, struct {
				id     types.NodeID
				proof  []byte
				domain int
			}{id, proof, domain})
			return true
		})
	require.NoError(t, err)
	require.Equal(t, len(ids), len(got))
	require.Equal(t, ids[0], got[0].id)
	require.Equal(t, proof, got[0].proof)
	require.Equal(t, 1, got[0].domain)

	ids = ids[1:]
	got = got[1:]
	for i, id := range ids {
		require.Equal(t, id, got[i].id)
		require.Nil(t, got[i].proof)
		require.Zero(t, got[i].domain)
	}
}
