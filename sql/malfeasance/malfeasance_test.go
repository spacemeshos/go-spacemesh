package malfeasance_test

import (
	"testing"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
)

func TestAdd(t *testing.T) {
	t.Parallel()

	db := sql.InMemory()

	id := types.RandomNodeID()
	domain := byte(1)
	proof := []byte{1, 2, 3}
	received := time.Now()

	require.NoError(t, malfeasance.Add(db, id, domain, proof, received))

	mal, err := malfeasance.IsMalicious(db, id)
	require.NoError(t, err)
	require.True(t, mal)
}

func TestAddMarried(t *testing.T) {
	t.Parallel()

	db := sql.InMemory()

	id := types.RandomNodeID()
	marriedTo := types.RandomNodeID()
	received := time.Now()

	require.NoError(t, malfeasance.Add(db, marriedTo, 1, []byte{1, 2, 3}, received))

	require.NoError(t, malfeasance.AddMarried(db, id, marriedTo, received))

	mal, err := malfeasance.IsMalicious(db, id)
	require.NoError(t, err)
	require.True(t, mal)

	mal, err = malfeasance.IsMalicious(db, marriedTo)
	require.NoError(t, err)
	require.True(t, mal)
}

func TestAddMarriedMissing(t *testing.T) {
	t.Parallel()

	db := sql.InMemory()

	id := types.RandomNodeID()
	marriedTo := types.RandomNodeID()
	received := time.Now()

	err := malfeasance.AddMarried(db, id, marriedTo, received)
	sqlError := &sqlite.Error{}
	require.ErrorAs(t, err, sqlError)
	require.Equal(t, sqlite.SQLITE_CONSTRAINT_FOREIGNKEY, sqlError.Code)

	mal, err := malfeasance.IsMalicious(db, id)
	require.NoError(t, err)
	require.False(t, mal)

	mal, err = malfeasance.IsMalicious(db, marriedTo)
	require.NoError(t, err)
	require.False(t, mal)
}
