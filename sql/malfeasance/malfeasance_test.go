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
