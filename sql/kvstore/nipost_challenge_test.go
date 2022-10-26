package kvstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAddNIPostChallenge(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	nipost := &types.NIPostChallenge{
		Sequence:       0,
		PositioningATX: types.RandomATXID(),
	}

	// Act
	require.NoError(t, AddNIPostChallenge(db, nipost))

	// Assert
	got, err := GetNIPostChallenge(db)
	require.NoError(t, err)
	require.Equal(t, nipost, got)
}

func TestOverwriteNIPostChallenge(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	nipost := &types.NIPostChallenge{
		Sequence:       0,
		PositioningATX: types.RandomATXID(),
	}
	newNipost := &types.NIPostChallenge{
		Sequence:       1,
		PositioningATX: types.RandomATXID(),
		PrevATXID:      types.RandomATXID(),
	}

	// Act
	require.NoError(t, AddNIPostChallenge(db, nipost))
	require.NoError(t, AddNIPostChallenge(db, newNipost))

	// Assert
	got, err := GetNIPostChallenge(db)
	require.NoError(t, err)
	require.Equal(t, newNipost, got)
}

func TestClearNIPostChallenge(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	nipost := &types.NIPostChallenge{
		Sequence:       0,
		PositioningATX: types.RandomATXID(),
	}
	require.NoError(t, AddNIPostChallenge(db, nipost))

	// Act
	require.NoError(t, ClearNIPostChallenge(db))

	// Assert
	got, err := GetNIPostChallenge(db)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)
}
