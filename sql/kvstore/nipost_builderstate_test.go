package kvstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAddNIPostBuilderState(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	state := &types.NIPostBuilderState{
		PoetRequests: []types.PoetRequest{
			{PoetRound: &types.PoetRound{ID: "asdf"}},
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
		PoetRequests: []types.PoetRequest{
			{PoetRound: &types.PoetRound{ID: "asdf"}},
		},
	}
	newState := &types.NIPostBuilderState{
		PoetRequests: []types.PoetRequest{
			{PoetRound: &types.PoetRound{ID: "1234"}},
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
		PoetRequests: []types.PoetRequest{
			{PoetRound: &types.PoetRound{ID: "asdf"}},
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
