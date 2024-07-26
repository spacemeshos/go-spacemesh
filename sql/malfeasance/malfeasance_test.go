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

func TestProof(t *testing.T) {
	t.Parallel()

	db := sql.InMemory()

	id := types.RandomNodeID()
	domain := byte(1)
	proof := []byte{1, 2, 3}
	received := time.Now()

	gotId, gotDomain, gotProof, err := malfeasance.Proof(db, id)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Zero(t, gotId)
	require.Zero(t, gotDomain)
	require.Nil(t, gotProof)

	require.NoError(t, malfeasance.Add(db, id, domain, proof, received))

	gotId, gotDomain, gotProof, err = malfeasance.Proof(db, id)
	require.NoError(t, err)
	require.Equal(t, id, gotId)
	require.Equal(t, domain, gotDomain)
	require.Equal(t, proof, gotProof)
}

func TestProofMarried(t *testing.T) {
	t.Parallel()

	db := sql.InMemory()

	id := types.RandomNodeID()
	marriedTo := types.RandomNodeID()
	domain := byte(1)
	proof := []byte{1, 2, 3}
	received := time.Now()

	require.NoError(t, malfeasance.Add(db, marriedTo, domain, proof, received))

	require.NoError(t, malfeasance.AddMarried(db, id, marriedTo, received))

	gotId, gotDomain, gotProof, err := malfeasance.Proof(db, marriedTo)
	require.NoError(t, err)
	require.Equal(t, marriedTo, gotId)
	require.Equal(t, domain, gotDomain)
	require.Equal(t, proof, gotProof)

	gotId, gotDomain, gotProof, err = malfeasance.Proof(db, id)
	require.NoError(t, err)
	require.Equal(t, marriedTo, gotId)
	require.Equal(t, domain, gotDomain)
	require.Equal(t, proof, gotProof)
}

func TestAll(t *testing.T) {
	t.Parallel()

	db := sql.InMemory()

	ids := make([]types.NodeID, 3)
	domain := byte(1)
	proof := []byte{1, 2, 3}
	received := time.Now()

	for i := range ids {
		ids[i] = types.RandomNodeID()
		require.NoError(t, malfeasance.Add(db, ids[i], domain, proof, received))
	}

	marriedIds := make([]types.NodeID, 3)
	for i := range marriedIds {
		marriedIds[i] = types.RandomNodeID()
		require.NoError(t, malfeasance.AddMarried(db, marriedIds[i], ids[i], received))
	}

	expected := append(ids, marriedIds...)
	all, err := malfeasance.All(db)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, all)
}
