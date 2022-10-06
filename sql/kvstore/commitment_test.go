package kvstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAddGetCommitmentATX(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()

	// Act
	require.NoError(t, AddCommitmentATX(db, atx))

	// Assert
	got, err := GetCommitmentATX(db)
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

func TestOverwriteCommitmentATX(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()
	newAtx := types.RandomATXID()

	// Act
	require.NoError(t, AddCommitmentATX(db, atx))
	require.NoError(t, AddCommitmentATX(db, newAtx))

	// Assert
	got, err := GetCommitmentATX(db)
	require.NoError(t, err)
	require.Equal(t, newAtx, got)
}

func TestClearCommitmentATX(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()
	require.NoError(t, AddCommitmentATX(db, atx))

	// Act
	require.NoError(t, ClearCommitmentAtx(db))

	// Assert
	got, err := GetCommitmentATX(db)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, *types.EmptyATXID, got)
}
