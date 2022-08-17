package atxs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const layersPerEpoch = 5

func TestGetATXByID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	atxs := make([]*types.ActivationTx, 0)
	for i := 0; i < 3; i++ {
		nodeID := types.NodeID{byte(i)}
		atxs = append(atxs, newAtx(nodeID, types.NewLayerID(uint32(i))))
	}

	for _, atx := range atxs {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	for _, want := range atxs {
		got, err := Get(db, want.ID())
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	_, err := Get(db, types.ATXID(types.CalcHash32([]byte("0"))))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestHasID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	atxs := make([]*types.ActivationTx, 0)
	for i := 0; i < 3; i++ {
		nodeID := types.NodeID{byte(i)}
		atxs = append(atxs, newAtx(nodeID, types.NewLayerID(uint32(i))))
	}

	for _, atx := range atxs {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	for _, atx := range atxs {
		has, err := Has(db, atx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}

	has, err := Has(db, types.ATXID(types.CalcHash32([]byte("0"))))
	require.NoError(t, err)
	require.False(t, has)
}

func TestGetTimestampByID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID := types.NodeID{1}
	atx := newAtx(nodeID, types.NewLayerID(uint32(0)))

	ts := time.Now()
	require.NoError(t, Add(db, atx, ts))

	timestamp, err := GetTimestamp(db, atx.ID())
	require.NoError(t, err)
	require.EqualValues(t, ts.UnixNano(), timestamp.UnixNano())

	_, err = GetTimestamp(db, types.ATXID(types.CalcHash32([]byte("0"))))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestGetLastIDByNodeID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID0 := types.NodeID{1}
	nodeID1 := types.NodeID{2}
	nodeID2 := types.NodeID{3}
	atx1 := newAtx(nodeID1, types.NewLayerID(uint32(1*layersPerEpoch)))
	atx2 := newAtx(nodeID1, types.NewLayerID(uint32(2*layersPerEpoch)))
	atx3 := newAtx(nodeID2, types.NewLayerID(uint32(3*layersPerEpoch)))
	atx4 := newAtx(nodeID2, types.NewLayerID(uint32(3*layersPerEpoch)))
	atx4.Sequence = atx3.Sequence + 1
	atx4.CalcAndSetID()

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	id1, err := GetLastIDByNodeID(db, nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), id1)

	id2, err := GetLastIDByNodeID(db, nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, atx4.ID(), id2)

	_, err = GetLastIDByNodeID(db, nodeID0)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestGetIDByEpochAndNodeID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID1 := types.NodeID{1}
	nodeID2 := types.NodeID{2}

	l1 := types.NewLayerID(uint32(1 * layersPerEpoch))
	l2 := types.NewLayerID(uint32(2 * layersPerEpoch))
	l3 := types.NewLayerID(uint32(3 * layersPerEpoch))

	atx1 := newAtx(nodeID1, l1)
	atx2 := newAtx(nodeID1, l2)
	atx3 := newAtx(nodeID2, l2)
	atx4 := newAtx(nodeID2, l3)

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	l1n1, err := GetIDByEpochAndNodeID(db, l1.GetEpoch(), nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, atx1.ID(), l1n1)

	_, err = GetIDByEpochAndNodeID(db, l1.GetEpoch(), nodeID2)
	require.ErrorIs(t, err, sql.ErrNotFound)

	l2n1, err := GetIDByEpochAndNodeID(db, l2.GetEpoch(), nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), l2n1)

	l2n2, err := GetIDByEpochAndNodeID(db, l2.GetEpoch(), nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, atx3.ID(), l2n2)

	_, err = GetIDByEpochAndNodeID(db, l3.GetEpoch(), nodeID1)
	require.ErrorIs(t, err, sql.ErrNotFound)

	l3n2, err := GetIDByEpochAndNodeID(db, l3.GetEpoch(), nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, atx4.ID(), l3n2)
}

func TestGetIDsByEpoch(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID1 := types.NodeID{1}
	nodeID2 := types.NodeID{2}

	l1 := types.NewLayerID(uint32(1 * layersPerEpoch))
	l2 := types.NewLayerID(uint32(2 * layersPerEpoch))
	l3 := types.NewLayerID(uint32(3 * layersPerEpoch))

	atx1 := newAtx(nodeID1, l1)
	atx2 := newAtx(nodeID1, l2)
	atx3 := newAtx(nodeID2, l2)
	atx4 := newAtx(nodeID2, l3)

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	ids1, err := GetIDsByEpoch(db, l1.GetEpoch())
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx1.ID()}, ids1)

	ids2, err := GetIDsByEpoch(db, l2.GetEpoch())
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx2.ID(), atx3.ID()}, ids2)

	ids3, err := GetIDsByEpoch(db, l3.GetEpoch())
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx4.ID()}, ids3)
}

func TestGetBlob(t *testing.T) {
	db := sql.InMemory()

	nodeID := types.NodeID{1}

	atx := newAtx(nodeID, types.NewLayerID(uint32(1)))

	require.NoError(t, Add(db, atx, time.Now()))
	buf, err := GetBlob(db, atx.ID().Bytes())
	require.NoError(t, err)
	encoded, err := codec.Encode(atx)
	require.NoError(t, err)
	require.Equal(t, encoded, buf)
}

func TestAdd(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nonExistingATXID := types.ATXID(types.CalcHash32([]byte("0")))
	_, err := Get(db, nonExistingATXID)
	require.ErrorIs(t, err, sql.ErrNotFound)

	nodeID := types.NodeID{1}
	atx := newAtx(nodeID, types.NewLayerID(uint32(1)))

	require.NoError(t, Add(db, atx, time.Now()))
	require.ErrorIs(t, Add(db, atx, time.Now()), sql.ErrObjectExists)

	got, err := Get(db, atx.ID())
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

func newAtx(nodeID types.NodeID, layerID types.LayerID) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			ActivationTxHeader: types.ActivationTxHeader{
				NIPostChallenge: types.NIPostChallenge{
					PubLayerID: layerID,
				},
				NumUnits: 2,
			},
		},
	}
	activationTx.Verify(0, 1)
	activationTx.CalcAndSetID()
	return activationTx
}

func TestPositioningID(t *testing.T) {
	types.SetLayersPerEpoch(10)
	type header struct {
		coinbase    types.Address
		base, count uint64
		epoch       types.EpochID
	}
	for _, tc := range []struct {
		desc   string
		atxs   []header
		expect int
	}{
		{
			desc: "not found",
		},
		{
			desc: "by epoch",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1},
				{coinbase: types.Address{2}, base: 1, count: 1, epoch: 2},
			},
			expect: 1,
		},
		{
			desc: "by tick height",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1},
				{coinbase: types.Address{2}, base: 1, count: 1, epoch: 1},
			},
			expect: 0,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			ids := []types.ATXID{}
			for _, atx := range tc.atxs {
				full := &types.ActivationTx{
					InnerActivationTx: types.InnerActivationTx{
						ActivationTxHeader: types.ActivationTxHeader{
							NIPostChallenge: types.NIPostChallenge{
								PubLayerID: atx.epoch.FirstLayer(),
							},
							Coinbase: atx.coinbase,
						},
					},
				}
				full.Verify(atx.base, atx.count)
				full.CalcAndSetID()
				require.NoError(t, Add(db, full, time.Time{}))
				ids = append(ids, full.ID())
			}
			rst, err := GetPositioningID(db)
			if len(tc.atxs) == 0 {
				require.ErrorIs(t, err, sql.ErrNotFound)
			} else {
				require.Equal(t, ids[tc.expect], rst)
			}
		})
	}
}
