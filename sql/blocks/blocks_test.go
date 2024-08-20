package blocks

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestAddGet(t *testing.T) {
	db := statesql.InMemory()
	block := types.NewExistingBlock(
		types.BlockID{1, 1},
		types.InnerBlock{LayerIndex: types.LayerID(1)},
	)

	require.NoError(t, Add(db, block))
	got, err := Get(db, block.ID())
	require.NoError(t, err)
	require.Equal(t, block, got)
}

func TestAlreadyExists(t *testing.T) {
	db := statesql.InMemory()
	block := types.NewExistingBlock(
		types.BlockID{1},
		types.InnerBlock{},
	)
	require.NoError(t, Add(db, block))
	require.ErrorIs(t, Add(db, block), sql.ErrObjectExists)
}

func TestHas(t *testing.T) {
	db := statesql.InMemory()
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
	db := statesql.InMemory()
	lid := types.LayerID(1)
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
	}
	require.NoError(t, SetValid(db, blocks[0].ID()))

	valid, err := IsValid(db, blocks[0].ID())
	require.NoError(t, err)
	require.True(t, valid)

	_, err = IsValid(db, blocks[1].ID())
	require.ErrorIs(t, err, ErrValidityNotDecided)

	_, err = IsValid(db, types.RandomBlockID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetInvalid(db, blocks[0].ID()))
	valid, err = IsValid(db, blocks[0].ID())
	require.NoError(t, err)
	require.False(t, valid)
}

func TestLayerFilter(t *testing.T) {
	db := statesql.InMemory()
	start := types.LayerID(1)
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
	db := statesql.InMemory()
	start := types.LayerID(1)
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
	db := statesql.InMemory()
	lid := types.LayerID(1)
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

	validities, err := ContextualValidity(db, lid)
	require.NoError(t, err)
	require.Len(t, validities, 3)

	for i, validity := range validities {
		require.Equal(t, blocks[i].ID(), validity.ID)
		require.False(t, validity.Validity)
		require.NoError(t, SetValid(db, validity.ID))
	}

	validities, err = ContextualValidity(db, lid)
	require.NoError(t, err)
	require.Len(t, validities, 3)
	for _, validity := range validities {
		require.True(t, validity.Validity)
		require.NoError(t, SetInvalid(db, validity.ID))
	}
}

func TestGetLayer(t *testing.T) {
	db := statesql.InMemory()
	lid1 := types.LayerID(11)
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

func TestLastValid(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		db := statesql.InMemory()
		_, err := LastValid(db)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
	t.Run("all valid", func(t *testing.T) {
		db := statesql.InMemory()
		blocks := map[types.BlockID]struct {
			lid types.LayerID
		}{
			{1}: {lid: 11},
			{2}: {lid: 22},
			{3}: {lid: 33},
		}
		for bid, layer := range blocks {
			block := types.NewExistingBlock(
				bid,
				types.InnerBlock{LayerIndex: layer.lid},
			)
			require.NoError(t, Add(db, block))
			require.NoError(t, SetValid(db, bid))
		}
		last, err := LastValid(db)
		require.NoError(t, err)
		require.Equal(t, 33, int(last))
	})
	t.Run("last is invalid", func(t *testing.T) {
		db := statesql.InMemory()
		blocks := map[types.BlockID]struct {
			invalid bool
			lid     types.LayerID
		}{
			{1}: {lid: 11},
			{2}: {lid: 22},
			{3}: {invalid: true, lid: 33},
		}
		for bid, layer := range blocks {
			block := types.NewExistingBlock(
				bid,
				types.InnerBlock{LayerIndex: layer.lid},
			)
			require.NoError(t, Add(db, block))
			if !layer.invalid {
				require.NoError(t, SetValid(db, bid))
			}
		}
		last, err := LastValid(db)
		require.NoError(t, err)
		require.Equal(t, 22, int(last))
	})
}

func TestLoadBlob(t *testing.T) {
	db := statesql.InMemory()
	ctx := context.Background()

	lid1 := types.LayerID(11)
	block1 := types.NewExistingBlock(
		types.BlockID{1, 1},
		types.InnerBlock{LayerIndex: lid1},
	)
	encoded1 := codec.MustEncode(block1)
	lid2 := lid1.Add(1)
	block2 := types.NewExistingBlock(
		types.BlockID{2, 2},
		types.InnerBlock{LayerIndex: lid2},
	)
	encoded2 := codec.MustEncode(block2)

	for _, b := range []*types.Block{block1, block2} {
		require.NoError(t, Add(db, b))
	}

	var blob1 sql.Blob
	require.NoError(t, LoadBlob(ctx, db, block1.ID().Bytes(), &blob1))
	require.Equal(t, encoded1, blob1.Bytes)

	var blob2 sql.Blob
	require.NoError(t, LoadBlob(ctx, db, block2.ID().Bytes(), &blob2))
	require.Equal(t, encoded2, blob2.Bytes)

	noSuchID := types.RandomBallotID()
	require.ErrorIs(t, LoadBlob(ctx, db, noSuchID.Bytes(), &sql.Blob{}), sql.ErrNotFound)

	sizes, err := GetBlobSizes(db, [][]byte{
		block1.ID().Bytes(),
		block2.ID().Bytes(),
		noSuchID.Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes), -1}, sizes)
}

func TestLayerForMangledBlock(t *testing.T) {
	db := statesql.InMemory()
	_, err := db.Exec("insert into blocks (id, layer, block) values (?1, ?2, ?3);",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, []byte(`mangled-block-id`))
			stmt.BindInt64(2, 1010101)
			stmt.BindBytes(3, []byte(`mangled-block`)) // this is actually should encode block
		}, nil)
	require.NoError(t, err)

	rst, err := Layer(db, types.LayerID(1010101))
	require.Empty(t, rst, 0)
	require.Error(t, err)
}
