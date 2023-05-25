package layers

import (
	"math"
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

	hashes := make(map[types.LayerID]types.Hash32)

	for i := 1; i < 100; i++ {
		lid := types.LayerID(i)
		hash := types.RandomHash()
		hashes[lid] = hash
		require.NoError(t, SetMeshHash(db, lid, hash))
	}

	t.Run("missing layers", func(t *testing.T) {
		from := types.LayerID(107)
		to := types.LayerID(111)
		by := uint32(1)
		got, err := GetAggHashes(db, from, to, by)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Empty(t, got)
	})

	t.Run("partially missing layers", func(t *testing.T) {
		from := types.LayerID(70)
		to := types.LayerID(111)
		by := uint32(1)
		got, err := GetAggHashes(db, from, to, by)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Empty(t, got)
	})

	tt := []struct {
		name   string
		from   types.LayerID
		to     types.LayerID
		by     uint32
		layers []types.LayerID
	}{
		{"from=to", types.LayerID(10), types.LayerID(10), 1, []types.LayerID{10}},
		{"from<to", types.LayerID(10), types.LayerID(20), 1, []types.LayerID{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}},
		{"from<to, by=2", types.LayerID(10), types.LayerID(20), 2, []types.LayerID{10, 12, 14, 16, 18, 20}},
		{"from<to, by=3", types.LayerID(10), types.LayerID(20), 3, []types.LayerID{10, 13, 16, 19, 20}},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetAggHashes(db, tc.from, tc.to, tc.by)
			require.NoError(t, err)
			require.Equal(t, len(tc.layers), len(got))
			for i, lid := range tc.layers {
				expected := hashes[lid]
				require.Equal(t, expected, got[i])
			}
		})
	}
}

func TestGetAggHashesOom(t *testing.T) {
	db := sql.InMemory()

	lids := make([]types.LayerID, 0, math.MaxUint32)
	for i := 0; i < math.MaxUint32; i++ {
		lids = append(lids, types.LayerID(i))
	}

	GetAggHashes(db, lids)
}
