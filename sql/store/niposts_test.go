package store

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

func TestAddNIPostBuilderState(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	state := &types.NIPostBuilderState{
		PoetRound: &types.PoetRound{
			ID: "asdf",
		},
	}

	// Act
	require.NoError(t, AddNIPostBuilderState(db, state))

	// Assert
	got, err := GetNIPostBuilderState(db)
	require.NoError(t, err)
	require.Equal(t, state, got)
}

func TestOverwriteNIPostBuilderState(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	state := &types.NIPostBuilderState{
		PoetRound: &types.PoetRound{
			ID: "asdf",
		},
	}
	newState := &types.NIPostBuilderState{
		PoetRound: &types.PoetRound{
			ID: "1234",
		},
	}

	// Act
	require.NoError(t, AddNIPostBuilderState(db, state))
	require.NoError(t, AddNIPostBuilderState(db, newState))

	// Assert
	got, err := GetNIPostBuilderState(db)
	require.NoError(t, err)
	require.Equal(t, newState, got)
}

func TestClearNIPostBuilderState(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	state := &types.NIPostBuilderState{
		PoetRound: &types.PoetRound{
			ID: "asdf",
		},
	}
	require.NoError(t, AddNIPostBuilderState(db, state))

	// Act
	require.NoError(t, ClearNIPostBuilderState(db))

	// Assert
	got, err := GetNIPostBuilderState(db)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)
}
