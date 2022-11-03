package blocks

import (
	"sort"
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

	require.NoError(t, Add(db, block))
	got, err := Get(db, block.ID())
	require.NoError(t, err)
	require.Equal(t, block, got)
}

func TestAlreadyExists(t *testing.T) {
	db := sql.InMemory()
	block := types.NewExistingBlock(
		types.BlockID{1},
		types.InnerBlock{},
	)
	require.NoError(t, Add(db, block))
	require.ErrorIs(t, Add(db, block), sql.ErrObjectExists)
}

func TestHas(t *testing.T) {
	db := sql.InMemory()
	block := types.NewExistingBlock(
		types.BlockID{1},
		types.InnerBlock{},
	)
	exists, err := Has(db, block.ID())
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, Add(db, block))
	exists, err = Has(db, block.ID())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestValidity(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(1)
	blocks := []*types.Block{
		types.NewExistingBlock(
			types.BlockID{1, 1},
			types.InnerBlock{LayerIndex: lid},
		),
		types.NewExistingBlock(
			types.BlockID{2, 2},
			types.InnerBlock{LayerIndex: lid},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, block))
		_, err := IsValid(db, block.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	}
	require.NoError(t, SetValid(db, blocks[0].ID()))
	valid, err := IsValid(db, blocks[0].ID())
	require.NoError(t, err)
	require.True(t, valid)

	require.NoError(t, SetInvalid(db, blocks[0].ID()))
	valid, err = IsValid(db, blocks[0].ID())
	require.NoError(t, err)
	require.False(t, valid)
}

func TestValidityIfNotSet(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(1)
	blocks := []*types.Block{
		types.NewExistingBlock(
			types.BlockID{1, 1},
			types.InnerBlock{LayerIndex: lid},
		),
		types.NewExistingBlock(
			types.BlockID{2, 2},
			types.InnerBlock{LayerIndex: lid},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, block))
		_, err := IsValid(db, block.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	}

	require.NoError(t, SetValidIfNotSet(db, blocks[0].ID()))
	v, err := IsValid(db, blocks[0].ID())
	require.NoError(t, err)
	require.True(t, v)
	require.ErrorIs(t, SetInvalidIfNotSet(db, blocks[0].ID()), sql.ErrNotFound)
	v, err = IsValid(db, blocks[0].ID())
	require.NoError(t, err)
	require.True(t, v)

	require.NoError(t, SetInvalidIfNotSet(db, blocks[1].ID()))
	v, err = IsValid(db, blocks[1].ID())
	require.NoError(t, err)
	require.False(t, v)
	require.ErrorIs(t, SetValidIfNotSet(db, blocks[1].ID()), sql.ErrNotFound)
	v, err = IsValid(db, blocks[1].ID())
	require.NoError(t, err)
	require.False(t, v)
}

func TestLayerFilter(t *testing.T) {
	db := sql.InMemory()
	start := types.NewLayerID(1)
	blocks := []*types.Block{
		types.NewExistingBlock(
			types.BlockID{1, 1},
			types.InnerBlock{LayerIndex: start},
		),
		types.NewExistingBlock(
			types.BlockID{3, 3},
			types.InnerBlock{LayerIndex: start.Add(1)},
		),
		types.NewExistingBlock(
			types.BlockID{2, 2},
			types.InnerBlock{LayerIndex: start},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, block))
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].ID().Compare(blocks[j].ID())
	})
	bids, err := IDsInLayer(db, start)
	require.NoError(t, err)
	require.Len(t, bids, 2)
	for i, bid := range bids {
		require.Equal(t, bid, blocks[i].ID())
	}

	blks, err := Layer(db, start)
	require.NoError(t, err)
	require.Len(t, blks, 2)
	require.ElementsMatch(t, blocks[:2], blks)
}

func TestLayerOrdered(t *testing.T) {
	db := sql.InMemory()
	start := types.NewLayerID(1)
	blocks := []*types.Block{
		types.NewExistingBlock(
			types.BlockID{1, 1},
			types.InnerBlock{LayerIndex: start},
		),
		types.NewExistingBlock(
			types.BlockID{3, 3},
			types.InnerBlock{LayerIndex: start},
		),
		types.NewExistingBlock(
			types.BlockID{2, 2},
			types.InnerBlock{LayerIndex: start},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, block))
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].ID().Compare(blocks[j].ID())
	})
	bids, err := IDsInLayer(db, start)
	require.NoError(t, err)
	require.Len(t, bids, 3)
	for i, bid := range bids {
		require.Equal(t, bid, blocks[i].ID())
	}
}

func TestContextualValidity(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(1)
	blocks := []*types.Block{
		types.NewExistingBlock(
			types.BlockID{1},
			types.InnerBlock{LayerIndex: lid},
		),
		types.NewExistingBlock(
			types.BlockID{2},
			types.InnerBlock{LayerIndex: lid},
		),
		types.NewExistingBlock(
			types.BlockID{3},
			types.InnerBlock{LayerIndex: lid},
		),
		types.NewExistingBlock(
			types.BlockID{4},
			types.InnerBlock{LayerIndex: lid.Add(1)},
		),
	}
	for _, block := range blocks {
		require.NoError(t, Add(db, block))
	}
	cnt, err := CountContextualValidity(db, lid)
	require.NoError(t, err)
	require.Zero(t, cnt)

	validities, err := ContextualValidity(db, lid)
	require.NoError(t, err)
	require.Len(t, validities, 3)

	for i, validity := range validities {
		require.Equal(t, blocks[i].ID(), validity.ID)
		require.False(t, validity.Validity)
		require.NoError(t, SetValid(db, validity.ID))
	}
	cnt, err = CountContextualValidity(db, lid)
	require.NoError(t, err)
	require.Equal(t, 3, cnt)

	validities, err = ContextualValidity(db, lid)
	require.NoError(t, err)
	require.Len(t, validities, 3)
	for _, validity := range validities {
		require.True(t, validity.Validity)
		require.NoError(t, SetInvalid(db, validity.ID))
	}
	cnt, err = CountContextualValidity(db, lid)
	require.NoError(t, err)
	require.Equal(t, 3, cnt)
}

func TestGetLayer(t *testing.T) {
	db := sql.InMemory()
	lid1 := types.NewLayerID(11)
	block1 := types.NewExistingBlock(
		types.BlockID{1, 1},
		types.InnerBlock{LayerIndex: lid1},
	)
	lid2 := lid1.Add(1)
	block2 := types.NewExistingBlock(
		types.BlockID{2, 2},
		types.InnerBlock{LayerIndex: lid2},
	)

	for _, b := range []*types.Block{block1, block2} {
		require.NoError(t, Add(db, b))
	}

	for _, b := range []*types.Block{block1, block2} {
		lid, err := GetLayer(db, b.ID())
		require.NoError(t, err)
		require.Equal(t, b.LayerIndex, lid)
	}
}
