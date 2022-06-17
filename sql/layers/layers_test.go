package layers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestHareOutput(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

	_, err := GetHareOutput(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetStatus(db, lid, Latest))

	_, err = GetHareOutput(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetHareOutput(db, lid, types.BlockID{}))
	output, err := GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, types.BlockID{}, output)

	expected := types.BlockID{1, 1, 1}
	require.NoError(t, SetHareOutput(db, lid, expected))
	output, err = GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, expected, output)
}

func TestWeakCoin(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

	_, err := GetWeakCoin(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetWeakCoin(db, lid, true))
	got, err := GetWeakCoin(db, lid)
	require.NoError(t, err)
	require.True(t, got)

	require.NoError(t, SetWeakCoin(db, lid, false))
	got, err = GetWeakCoin(db, lid)
	require.NoError(t, err)
	require.False(t, got)
}

func TestAppliedBlock(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

	_, err := GetApplied(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	// cause layer to be inserted
	require.NoError(t, SetHareOutput(db, lid, types.BlockID{}))

	_, err = GetApplied(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetApplied(db, lid, types.EmptyBlockID))
	output, err := GetApplied(db, lid)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, output)

	expected := types.BlockID{1, 1, 1}
	require.NoError(t, SetApplied(db, lid, expected))
	output, err = GetApplied(db, lid)
	require.NoError(t, err)
	require.Equal(t, expected, output)

	require.NoError(t, UnsetAppliedFrom(db, lid))
	_, err = GetApplied(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestUnsetAppliedFrom(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)
	last := lid.Add(99)
	for i := lid; !i.After(last); i = i.Add(1) {
		require.NoError(t, SetApplied(db, i, types.EmptyBlockID))
		got, err := GetLastApplied(db)
		require.NoError(t, err)
		require.Equal(t, i, got)
	}
	require.NoError(t, UnsetAppliedFrom(db, lid.Add(1)))
	got, err := GetLastApplied(db)
	require.NoError(t, err)
	require.Equal(t, lid, got)
}

func TestStatus(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

	require.NoError(t, SetStatus(db, lid, Applied))
	require.NoError(t, SetStatus(db, lid.Add(1), Processed))
	require.NoError(t, SetStatus(db, lid.Add(2), Processed))
	require.NoError(t, SetStatus(db, lid.Add(3), Latest))

	processed, err := GetByStatus(db, Processed)
	require.NoError(t, err)
	require.Equal(t, lid.Add(2), processed)

	latest, err := GetByStatus(db, Latest)
	require.NoError(t, err)
	require.Equal(t, lid.Add(3), latest)

	applied, err := GetByStatus(db, Applied)
	require.NoError(t, err)
	require.Equal(t, lid, applied)
}

func TestStateHash(t *testing.T) {
	db := sql.InMemory()
	layers := []uint32{9, 10, 8, 7}
	hashes := []types.Hash32{{1}, {2}, {3}, {4}}
	for i := range layers {
		require.NoError(t, UpdateStateHash(db, types.NewLayerID(layers[i]), hashes[i]))
	}

	for i, lid := range layers {
		hash, err := GetStateHash(db, types.NewLayerID(lid))
		require.NoError(t, err)
		require.Equal(t, hashes[i], hash)
	}

	latest, err := GetLatestStateHash(db)
	require.NoError(t, err)
	require.Equal(t, hashes[1], latest)

	require.NoError(t, UnsetAppliedFrom(db, types.NewLayerID(layers[1])))
	latest, err = GetLatestStateHash(db)
	require.NoError(t, err)
	require.Equal(t, hashes[0], latest)
}
