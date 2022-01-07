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
