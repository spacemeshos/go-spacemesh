package identity_test

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func Test_StatesPersistance(t *testing.T) {
	db := localsql.InMemoryTest(t)

	storage1 := identity.NewIdentityStateStorage(db)

	// Set some states
	id1 := types.NodeID{1}
	id2 := types.NodeID{2}

	states := []struct {
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

	// Store states in first storage
	for _, s := range states {
		storage1.Set(s.id, s.state)
	}
	states1 := storage1.All()

	// Create new storage instance with same DB
	storage2 := identity.NewIdentityStateStorage(db)
	states2 := storage2.All()

	for id, states1 := range states1 {
		for i, state := range states1 {
			identity.RequireEqual(t, &state, &states2[id][i])
		}
	}
}
