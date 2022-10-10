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
	nodeId := types.NodeID{0x0, 0x1}

	// Act
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId))

	// Assert
	got, err := GetCommitmentATXForNode(db, nodeId)
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

func TestOverwriteCommitmentATX(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()
	newAtx := types.RandomATXID()
	nodeId := types.NodeID{0x0, 0x1}

	// Act
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId))
	require.NoError(t, AddCommitmentATXForNode(db, newAtx, nodeId))

	// Assert
	got, err := GetCommitmentATXForNode(db, nodeId)
	require.NoError(t, err)
	require.Equal(t, newAtx, got)
}

func TestNotOverwriteCommitmentATXFromOtherNodeID(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()
	newAtx := types.RandomATXID()
	nodeId := types.NodeID{0x0, 0x1}
	nodeId2 := types.NodeID{0x0, 0x2}

	// Act
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId))
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId2))
	require.NoError(t, AddCommitmentATXForNode(db, newAtx, nodeId))

	// Assert
	got, err := GetCommitmentATXForNode(db, nodeId)
	require.NoError(t, err)
	require.Equal(t, newAtx, got)

	got, err = GetCommitmentATXForNode(db, nodeId2)
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

func TestClearCommitmentATX(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()
	nodeId := types.NodeID{0x0, 0x1}
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId))

	// Act
	require.NoError(t, ClearCommitmentAtx(db, nodeId))

	// Assert
	got, err := GetCommitmentATXForNode(db, nodeId)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, *types.EmptyATXID, got)
}

func TestNotClearCommitmentATXFromOtherNodeID(t *testing.T) {
	// Arrange
	db := sql.InMemory()
	atx := types.RandomATXID()
	nodeId := types.NodeID{0x0, 0x1}
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId))

	nodeId2 := types.NodeID{0x0, 0x2}
	require.NoError(t, AddCommitmentATXForNode(db, atx, nodeId2))

	// Act
	require.NoError(t, ClearCommitmentAtx(db, nodeId))

	// Assert
	got, err := GetCommitmentATXForNode(db, nodeId)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, *types.EmptyATXID, got)

	got, err = GetCommitmentATXForNode(db, nodeId2)
	require.NoError(t, err)
	require.Equal(t, atx, got)
}
