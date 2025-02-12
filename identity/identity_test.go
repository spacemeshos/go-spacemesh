package identity_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

var (
	id1    = types.NodeID{1}
	id2    = types.NodeID{2}
	states = []struct {
		id    types.NodeID
		state identity.State
	}{
		{
			id:    id1,
			state: &identity.Retrying{ErrorMsg: "ID1 error"},
		},
		{
			id:    id1,
			state: &identity.ATXReady{},
		},
		{
			id:    id2,
			state: &identity.Retrying{ErrorMsg: "ID2 failed"},
		},
		{
			id: id1,
			state: &identity.ATXBroadcasted{
				AtxId: types.RandomATXID(),
			},
		},
	}
)

func Test_StatesPersistance(t *testing.T) {
	db := localsql.InMemoryTest(t)

	storage1 := identity.NewIdentityStateStorage(db, zaptest.NewLogger(t))

	// Store states in first storage
	for _, s := range states {
		storage1.Set(s.id, s.state)
	}
	ops := builder.Operations{
		Filter: []builder.Op{},
		Modifiers: []builder.Modifier{
			{
				Key:   builder.Limit,
				Value: int64(10),
			},
		},
	}
	states1, err := storage1.All(ops)
	require.NoError(t, err)

	// Create new storage instance with same DB
	storage2 := identity.NewIdentityStateStorage(db, zaptest.NewLogger(t))
	states2, err := storage2.All(ops)
	require.NoError(t, err)

	require.Equal(t, states1, states2)
}

func Test_StatesOperations(t *testing.T) {
	setup := func(t *testing.T) *identity.StateStorage {
		db := localsql.InMemoryTest(t)
		storage := identity.NewIdentityStateStorage(db, zaptest.NewLogger(t))
		// Store states in first storage
		for _, s := range states {
			storage.Set(s.id, s.state)
		}

		return storage
	}

	t.Run("limit and offset", func(t *testing.T) {
		storage := setup(t)
		ops := builder.Operations{
			Filter: []builder.Op{},
			Modifiers: []builder.Modifier{
				{
					Key:   builder.Limit,
					Value: int64(2),
				},
				{
					Key:   builder.Offset,
					Value: int64(1),
				},
			},
		}

		rst, err := storage.All(ops)
		require.NoError(t, err)
		require.Len(t, rst, 2)
		require.Equal(t, states[1].state, rst[0].State)
		require.Equal(t, states[2].state, rst[1].State)
	})

	t.Run("filter by state", func(t *testing.T) {
		storage := setup(t)
		ops := builder.Operations{
			Filter: []builder.Op{
				{
					Field: "kind",
					Token: builder.In,
					// 11 is IdentityState_ATX_BROADCASTED
					Value: []int32{11},
				},
			},
		}

		rst, err := storage.All(ops)
		require.NoError(t, err)
		require.Len(t, rst, 1)
		require.Equal(t, states[3].state, rst[0].State)
	})
}
