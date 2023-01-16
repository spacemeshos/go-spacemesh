package vrfnonce

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestGetNonceForEpoch(t *testing.T) {
	// Arrange
	db := sql.InMemory()

	nodeID := types.RandomNodeID()
	epochID1 := types.EpochID(20)
	nonce1 := types.VRFPostIndex(333)

	epochID2 := types.EpochID(50)
	nonce2 := types.VRFPostIndex(777)

	err := Add(db, nodeID, epochID1, nonce1)
	require.NoError(t, err)

	err = Add(db, nodeID, epochID2, nonce2)
	require.NoError(t, err)

	// Act & Assert

	// same epoch returns same nonce
	got, err := Get(db, nodeID, epochID1)
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	got, err = Get(db, nodeID, epochID2)
	require.NoError(t, err)
	require.Equal(t, nonce2, got)

	// between epochs returns previous nonce
	got, err = Get(db, nodeID, epochID1+10)
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	// later epoch returns newer nonce
	got, err = Get(db, nodeID, epochID2+10)
	require.NoError(t, err)
	require.Equal(t, nonce2, got)

	// before first epoch returns error
	_, err = Get(db, nodeID, epochID1-10)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAdd(t *testing.T) {
	// Arrange
	db := sql.InMemory()

	nodeID := types.RandomNodeID()
	epochID := types.EpochID(1)
	nonce := types.VRFPostIndex(999)

	// Act
	err := Add(db, nodeID, epochID, nonce)
	require.NoError(t, err)

	// Assert
	got, err := Get(db, nodeID, epochID)
	require.NoError(t, err)
	require.Equal(t, nonce, got)
}
