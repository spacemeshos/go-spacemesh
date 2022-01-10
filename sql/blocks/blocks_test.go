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

	valid, err = IsVerified(db, blocks[1].ID())
	require.NoError(t, err)
	require.False(t, valid)
}
