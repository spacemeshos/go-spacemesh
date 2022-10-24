package layers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func makeCert(lid types.LayerID, bid types.BlockID) *types.Certificate {
	return &types.Certificate{
		BlockID: bid,
		Signatures: []types.CertifyMessage{
			{
				CertifyContent: types.CertifyContent{
					LayerID:        lid,
					BlockID:        bid,
					EligibilityCnt: 1,
					Proof:          []byte("not a fraud 1"),
				},
			},
			{
				CertifyContent: types.CertifyContent{
					LayerID:        lid,
					BlockID:        bid,
					EligibilityCnt: 2,
					Proof:          []byte("not a fraud 2"),
				},
			},
		},
	}
}

func TestHareOutput(t *testing.T) {
	db := sql.InMemory()
	lid1 := types.NewLayerID(10)

	_, err := GetHareOutput(db, lid1)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetHareOutput(db, lid1, types.BlockID{}))
	output, err := GetHareOutput(db, lid1)
	require.NoError(t, err)
	require.Equal(t, types.BlockID{}, output)

	// setting the same layer for the second time will not have any effect
	require.NoError(t, SetHareOutput(db, lid1, types.RandomBlockID()))
	output, err = GetHareOutput(db, lid1)
	require.NoError(t, err)
	require.Equal(t, types.BlockID{}, output)

	// but setting the same layer with a certificate works
	bid1 := types.BlockID{1, 1, 1}
	cert1 := makeCert(lid1, bid1)
	require.NoError(t, SetHareOutputWithCert(db, lid1, cert1))

	output, err = GetHareOutput(db, lid1)
	require.NoError(t, err)
	require.Equal(t, bid1, output)
	gotC, err := GetCert(db, lid1)
	require.NoError(t, err)
	require.Equal(t, cert1, gotC)

	bid2 := types.BlockID{2, 2, 2}
	lid2 := lid1.Add(1)
	cert2 := makeCert(lid2, bid2)
	require.NoError(t, SetHareOutputWithCert(db, lid2, cert2))
	output, err = GetHareOutput(db, lid2)
	require.NoError(t, err)
	require.Equal(t, bid2, output)
	gotC, err = GetCert(db, lid2)
	require.NoError(t, err)
	require.Equal(t, cert2, gotC)

	bid3 := types.BlockID{3, 3, 3}
	cert3 := makeCert(lid2, bid3)
	// will not overwrite the previous certificate
	require.NoError(t, SetHareOutput(db, lid2, bid3))
	require.NoError(t, SetHareOutputWithCert(db, lid2, cert3))
	output, err = GetHareOutput(db, lid2)
	require.NoError(t, err)
	require.Equal(t, bid2, output)
	gotC, err = GetCert(db, lid2)
	require.NoError(t, err)
	require.Equal(t, cert2, gotC)

	got, err := GetCert(db, lid2.Add(1))
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)
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

func TestSetHashes(t *testing.T) {
	db := sql.InMemory()
	_, err := GetHash(db, types.NewLayerID(11))
	require.ErrorIs(t, err, sql.ErrNotFound)
	_, err = GetAggregatedHash(db, types.NewLayerID(11))
	require.ErrorIs(t, err, sql.ErrNotFound)

	layers := []uint32{9, 10, 8, 7}
	hashes := []types.Hash32{{1}, {2}, {3}, {4}}
	aggHashes := []types.Hash32{{5}, {6}, {7}, {8}}
	for i := range layers {
		require.NoError(t, SetHashes(db, types.NewLayerID(layers[i]), hashes[i], aggHashes[i]))
	}

	for i, lid := range layers {
		hash, err := GetHash(db, types.NewLayerID(lid))
		require.NoError(t, err)
		require.Equal(t, hashes[i], hash)
		aggHash, err := GetAggregatedHash(db, types.NewLayerID(lid))
		require.NoError(t, err)
		require.Equal(t, aggHashes[i], aggHash)
	}

	require.NoError(t, UnsetAppliedFrom(db, types.NewLayerID(layers[0])))
	for i, lid := range layers {
		if i < 2 {
			got, err := GetHash(db, types.NewLayerID(lid))
			require.NoError(t, err)
			require.Equal(t, types.EmptyLayerHash, got)
			got, err = GetAggregatedHash(db, types.NewLayerID(lid))
			require.NoError(t, err)
			require.Equal(t, types.EmptyLayerHash, got)
		} else {
			hash, err := GetHash(db, types.NewLayerID(lid))
			require.NoError(t, err)
			require.Equal(t, hashes[i], hash)
			aggHash, err := GetAggregatedHash(db, types.NewLayerID(lid))
			require.NoError(t, err)
			require.Equal(t, aggHashes[i], aggHash)
		}
	}
}

func TestProcessed(t *testing.T) {
	db := sql.InMemory()
	lid, err := GetProcessed(db)
	require.NoError(t, err)
	require.Equal(t, types.LayerID{}, lid)
	layers := []uint32{9, 10, 8, 7}
	expected := []uint32{9, 10, 10, 10}
	for i := range layers {
		require.NoError(t, SetProcessed(db, types.NewLayerID(layers[i])))
		lid, err = GetProcessed(db)
		require.NoError(t, err)
		require.Equal(t, types.NewLayerID(expected[i]), lid)
	}
}

func TestGetAggHashes(t *testing.T) {
	db := sql.InMemory()

	lids := []types.LayerID{types.NewLayerID(9), types.NewLayerID(11), types.NewLayerID(7)}
	got, err := GetAggHashes(db, lids)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, got)

	hashes := []types.Hash32{{1}, {2}, {3}}
	aggHashes := []types.Hash32{{6}, {7}, {8}}
	expected := []types.Hash32{{8}, {6}, {7}} // in layer order
	for i, lid := range lids {
		require.NoError(t, SetHashes(db, lid, hashes[i], aggHashes[i]))
	}

	got, err = GetAggHashes(db, lids)
	require.NoError(t, err)
	require.Equal(t, expected, got)
}
