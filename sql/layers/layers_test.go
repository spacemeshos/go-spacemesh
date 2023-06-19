package layers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestWeakCoin(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(10)

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
	lid := types.LayerID(10)

	_, err := GetApplied(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	// cause layer to be inserted
	require.NoError(t, SetWeakCoin(db, lid, false))

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
	lid := types.LayerID(10)
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

func TestStateHash(t *testing.T) {
	db := sql.InMemory()
	layers := []uint32{9, 10, 8, 7}
	hashes := []types.Hash32{{1}, {2}, {3}, {4}}
	for i := range layers {
		require.NoError(t, UpdateStateHash(db, types.LayerID(layers[i]), hashes[i]))
	}

	for i, lid := range layers {
		hash, err := GetStateHash(db, types.LayerID(lid))
		require.NoError(t, err)
		require.Equal(t, hashes[i], hash)
	}

	latest, err := GetLatestStateHash(db)
	require.NoError(t, err)
	require.Equal(t, hashes[1], latest)

	require.NoError(t, UnsetAppliedFrom(db, types.LayerID(layers[1])))
	latest, err = GetLatestStateHash(db)
	require.NoError(t, err)
	require.Equal(t, hashes[0], latest)
}

func TestSetHashes(t *testing.T) {
	db := sql.InMemory()
	_, err := GetAggregatedHash(db, types.LayerID(11))
	require.ErrorIs(t, err, sql.ErrNotFound)

	layers := []uint32{9, 10, 8, 7}
	aggHashes := []types.Hash32{{5}, {6}, {7}, {8}}
	for i := range layers {
		require.NoError(t, SetMeshHash(db, types.LayerID(layers[i]), aggHashes[i]))
	}

	for i, lid := range layers {
		aggHash, err := GetAggregatedHash(db, types.LayerID(lid))
		require.NoError(t, err)
		require.Equal(t, aggHashes[i], aggHash)
	}

	require.NoError(t, UnsetAppliedFrom(db, types.LayerID(layers[0])))
	for i, lid := range layers {
		if i < 2 {
			got, err := GetAggregatedHash(db, types.LayerID(lid))
			require.NoError(t, err)
			require.Equal(t, types.EmptyLayerHash, got)
		} else {
			aggHash, err := GetAggregatedHash(db, types.LayerID(lid))
			require.NoError(t, err)
			require.Equal(t, aggHashes[i], aggHash)
		}
	}
}

func TestProcessed(t *testing.T) {
	db := sql.InMemory()
	lid, err := GetProcessed(db)
	require.NoError(t, err)
	require.Equal(t, types.LayerID(0), lid)
	layers := []uint32{9, 10, 8, 7}
	expected := []uint32{9, 10, 10, 10}
	for i := range layers {
		require.NoError(t, SetProcessed(db, types.LayerID(layers[i])))
		lid, err = GetProcessed(db)
		require.NoError(t, err)
		require.Equal(t, types.LayerID(expected[i]), lid)
	}
}

func TestGetAggHashes(t *testing.T) {
	db := sql.InMemory()

	lids := []types.LayerID{types.LayerID(9), types.LayerID(11), types.LayerID(7)}
	got, err := GetAggHashes(db, lids)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, got)

	aggHashes := []types.Hash32{{6}, {7}, {8}}
	expected := []types.Hash32{{8}, {6}, {7}} // in layer order
	for i, lid := range lids {
		require.NoError(t, SetMeshHash(db, lid, aggHashes[i]))
	}

	got, err = GetAggHashes(db, lids)
	require.NoError(t, err)
	require.Equal(t, expected, got)
}
