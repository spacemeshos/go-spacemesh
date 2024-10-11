package malfeasance_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestAdd(t *testing.T) {
	t.Parallel()

	t.Run("without proof and marriage ID fails", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		err := malfeasance.AddProof(db, types.RandomNodeID(), nil, nil, 0, time.Now())
		require.Error(t, err)
	})

	t.Run("with proof and without marriage ID succeeds", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		err := malfeasance.AddProof(db, types.RandomNodeID(), nil, types.RandomBytes(100), 0, time.Now())
		require.NoError(t, err)
	})

	t.Run("without proof and with unknown marriage ID fails", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		id := marriage.ID(100)
		err := malfeasance.AddProof(db, types.RandomNodeID(), &id, nil, 0, time.Now())
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

		err = malfeasance.AddProof(db, types.RandomNodeID(), &id, nil, 0, time.Now())
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

		err = malfeasance.AddProof(db, types.RandomNodeID(), &id, types.RandomBytes(100), 0, time.Now())
		require.NoError(t, err)
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
		err := malfeasance.AddProof(db, nodeID, nil, types.RandomBytes(100), 0, time.Now())
		require.NoError(t, err)

		mal, err := malfeasance.IsMalicious(db, nodeID)
		require.NoError(t, err)
		require.True(t, mal)
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
		err := malfeasance.AddProof(db, nodeID, nil, types.RandomBytes(100), 0, time.Now())
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
		err = malfeasance.AddProof(db, nodeID, nil, types.RandomBytes(100), 0, time.Now())
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
