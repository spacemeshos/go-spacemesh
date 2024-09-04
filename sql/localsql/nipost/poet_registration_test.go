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
		NodeId:        nodeID,
		ChallengeHash: types.RandomHash(),
		Address:       "address1",
		RoundID:       "round1",
		RoundEnd:      time.Now().Round(time.Second),
	}
	reg2 := PoETRegistration{
		NodeId:        nodeID,
		ChallengeHash: types.RandomHash(),
		Address:       "address2",
		RoundID:       "round2",
		RoundEnd:      time.Now().Round(time.Second),
	}

	err := AddPoetRegistration(db, reg1)
	require.NoError(t, err)

	registrations, err := PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 1)

	err = AddPoetRegistration(db, reg2)
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
		NodeId:        nodeID,
		ChallengeHash: types.RandomHash(),
		Address:       "address1",
		RoundID:       "round1",
		RoundEnd:      time.Now().Round(time.Second),
	}

	err := AddPoetRegistration(db, reg)
	require.NoError(t, err)

	registrations, err := PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 1)

	err = AddPoetRegistration(db, reg)
	require.ErrorIs(t, err, sql.ErrObjectExists)

	registrations, err = PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, registrations, 1)
}

func Test_UpdatePoetRegistrations(t *testing.T) {
	db := localsql.InMemory()

	nodeID := types.RandomNodeID()
	challengeHash := types.RandomHash()

	const (
		address = "address1"
		roundId = "1"
	)

	failedReg := PoETRegistration{
		NodeId:        nodeID,
		ChallengeHash: challengeHash,
		Address:       address,
		RoundEnd:      time.Time{}.Local(),
	}

	err := AddPoetRegistration(db, failedReg)
	require.NoError(t, err)

	regs, err := PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, failedReg.RoundID, regs[0].RoundID)
	require.Equal(t, failedReg.RoundEnd, regs[0].RoundEnd)

	failedReg.RoundID = roundId
	failedReg.RoundEnd = time.Now().Round(time.Second)

	err = AddPoetRegistration(db, failedReg) // update failed reg
	require.NoError(t, err)

	regs, err = PoetRegistrations(db, nodeID)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, failedReg.RoundID, regs[0].RoundID)
	require.Equal(t, failedReg.RoundEnd, regs[0].RoundEnd)
}
