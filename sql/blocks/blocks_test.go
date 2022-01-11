package blocks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAddGet(t *testing.T) {
	db := sql.InMemory()
	block := types.NewExistingBlock(
		types.BlockID{1, 1},
		types.InnerBlock{LayerIndex: types.NewLayerID(1)},
	)

	require.NoError(t, Add(db, &block))
	got, err := Get(db, block.ID())
	require.NoError(t, err)
	require.Equal(t, &block, got)
}

func TestAlreadyExists(t *testing.T) {
	db := sql.InMemory()
	blocks := []types.Block{
		types.NewExistingBlock(
			types.BlockID{1},
			types.InnerBlock{},
		),
		types.NewExistingBlock(
			types.BlockID{1},
			types.InnerBlock{},
		),
	}
	require.NoError(t, Add(db, &blocks[0]))
	require.ErrorIs(t, Add(db, &blocks[1]), sql.ErrObjectExists)
}

func TestVerified(t *testing.T) {
	db := sql.InMemory()
	blocks := []types.Block{
		types.NewExistingBlock(
			types.BlockID{1, 1},
			types.InnerBlock{},
		),
		types.NewExistingBlock(
			types.BlockID{2, 2},
			types.InnerBlock{},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, &block))
	}
	require.NoError(t, SetVerified(db, blocks[0].ID()))

	valid, err := IsVerified(db, blocks[0].ID())
	require.NoError(t, err)
	require.True(t, valid)

	_, err = IsVerified(db, blocks[1].ID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetInvalid(db, blocks[0].ID()))
	valid, err = IsVerified(db, blocks[0].ID())
	require.NoError(t, err)
	require.False(t, valid)
}

func TestLayerFilter(t *testing.T) {
	db := sql.InMemory()
	start := types.NewLayerID(1)
	blocks := []types.Block{
		types.NewExistingBlock(
			types.BlockID{1, 1},
			types.InnerBlock{LayerIndex: start},
		),
		types.NewExistingBlock(
			types.BlockID{2, 2},
			types.InnerBlock{LayerIndex: start},
		),
		types.NewExistingBlock(
			types.BlockID{3, 3},
			types.InnerBlock{LayerIndex: start.Add(1)},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, &block))
	}
	bids, err := Layer(db, start)
	require.NoError(t, err)
	require.Len(t, bids, 2)
	for i, bid := range bids {
		require.Equal(t, bid, blocks[i].ID())
	}
}
