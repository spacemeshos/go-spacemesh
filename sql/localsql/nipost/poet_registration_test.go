package nipost

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func Test_AddPoetRegistration(t *testing.T) {
	db := localsql.InMemory()

	nodeID := types.RandomNodeID()
	reg1 := PoETRegistration{
		ChallengeHash: types.RandomHash(),
		Address:       "address1",
		RoundID:       "round1",
		RoundEnd:      time.Now().Round(time.Second),
	}
	reg2 := PoETRegistration{
		ChallengeHash: types.RandomHash(),
		Address:       "address2",
		RoundID:       "round2",
		RoundEnd:      time.Now().Round(time.Second),
	}

	err := AddPoetRegistration(db, nodeID, reg1)
	require.NoError(t, err)

	registrations, err := PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 1)

	err = AddPoetRegistration(db, nodeID, reg2)
	require.NoError(t, err)

	registrations, err = PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 2)
	require.Equal(t, reg1, registrations[0])
	require.Equal(t, reg2, registrations[1])

	err = ClearPoetRegistrations(db, nodeID)
	require.NoError(t, err)

	registrations, err = PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Empty(t, registrations)
}

func Test_AddPoetRegistration_NoDuplicates(t *testing.T) {
	db := localsql.InMemory()

	nodeID := types.RandomNodeID()
	reg := PoETRegistration{
		ChallengeHash: types.RandomHash(),
		Address:       "address1",
		RoundID:       "round1",
		RoundEnd:      time.Now().Round(time.Second),
	}

	err := AddPoetRegistration(db, nodeID, reg)
	require.NoError(t, err)

	registrations, err := PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 1)

	err = AddPoetRegistration(db, nodeID, reg)
	require.ErrorIs(t, err, sql.ErrObjectExists)

	registrations, err = PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 1)
}
