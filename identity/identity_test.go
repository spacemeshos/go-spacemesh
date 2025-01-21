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
	states1 := storage1.All(ops)

	// Create new storage instance with same DB
	storage2 := identity.NewIdentityStateStorage(db, zaptest.NewLogger(t))
	states2 := storage2.All(ops)

	for id, states1 := range states1 {
		for i, state := range states1 {
			identity.RequireEqual(t, &state, &states2[id][i])
		}
	}
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

		rst := storage.All(ops)
		require.Len(t, rst, 2)
		require.Equal(t, states[2].state, rst[id2][0].State)
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
			Modifiers: []builder.Modifier{
				{
					Key:   builder.Limit,
					Value: int64(10),
				},
			},
		}

		rst := storage.All(ops)
		require.Len(t, rst, 1)
		require.Equal(t, states[3].state, rst[id1][0].State)
	})
}
