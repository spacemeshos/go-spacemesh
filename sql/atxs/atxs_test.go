package atxs

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const layersPerEpoch = 5

func TestGetATXByID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	atxs := make([]*types.ActivationTx, 0)
	for i := 0; i < 3; i++ {
		strIdx := strconv.Itoa(i)
		nodeID := types.NodeID{Key: strIdx, VRFPublicKey: []byte(strIdx)}
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
		strIdx := strconv.Itoa(i)
		nodeID := types.NodeID{Key: strIdx, VRFPublicKey: []byte(strIdx)}
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

	nodeID := types.NodeID{Key: "0", VRFPublicKey: []byte("0")}
	atx := newAtx(nodeID, types.NewLayerID(uint32(0)))

	ts := time.Now()
	require.NoError(t, Add(db, atx, ts))

	timestamp, err := GetTimestamp(db, atx.ID())
	require.NoError(t, err)
	require.EqualValues(t, ts.UnixNano(), timestamp.UnixNano())

	_, err = GetTimestamp(db, types.ATXID(types.CalcHash32([]byte("0"))))
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestGetLastIDByNodeID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID0 := types.NodeID{Key: "0", VRFPublicKey: []byte("0")}
	nodeID1 := types.NodeID{Key: "1", VRFPublicKey: []byte("1")}
	nodeID2 := types.NodeID{Key: "1", VRFPublicKey: []byte("2")}
	atx1 := newAtx(nodeID1, types.NewLayerID(uint32(1*layersPerEpoch)))
	atx2 := newAtx(nodeID1, types.NewLayerID(uint32(2*layersPerEpoch)))
	atx3 := newAtx(nodeID2, types.NewLayerID(uint32(3*layersPerEpoch)))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3} {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	id1, err := GetLastIDByNodeID(db, nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), id1)

	id2, err := GetLastIDByNodeID(db, nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, atx3.ID(), id2)

	_, err = GetLastIDByNodeID(db, nodeID0)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestGetIDByEpochAndNodeID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID1 := types.NodeID{Key: "1", VRFPublicKey: []byte("1")}
	nodeID2 := types.NodeID{Key: "1", VRFPublicKey: []byte("2")}

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
	require.ErrorIs(t, err, database.ErrNotFound)

	l2n1, err := GetIDByEpochAndNodeID(db, l2.GetEpoch(), nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), l2n1)

	l2n2, err := GetIDByEpochAndNodeID(db, l2.GetEpoch(), nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, atx3.ID(), l2n2)

	_, err = GetIDByEpochAndNodeID(db, l3.GetEpoch(), nodeID1)
	require.ErrorIs(t, err, database.ErrNotFound)

	l3n2, err := GetIDByEpochAndNodeID(db, l3.GetEpoch(), nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, atx4.ID(), l3n2)
}

func TestGetIDsByEpoch(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID1 := types.NodeID{Key: "1", VRFPublicKey: []byte("1")}
	nodeID2 := types.NodeID{Key: "1", VRFPublicKey: []byte("2")}

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

func TestGetTop(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()

	nodeID1 := types.NodeID{Key: "1", VRFPublicKey: []byte("1")}
	nodeID2 := types.NodeID{Key: "1", VRFPublicKey: []byte("2")}

	l1 := types.NewLayerID(uint32(1 * layersPerEpoch))
	l2 := types.NewLayerID(uint32(2 * layersPerEpoch))

	atx1 := newAtx(nodeID1, l1)
	atx2 := newAtx(nodeID1, l2)
	atx3 := newAtx(nodeID2, l2)

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3} {
		require.NoError(t, Add(db, atx, time.Now()))
	}

	top, err := GetTop(db)
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), top)
}

func TestGetBlob(t *testing.T) {
	db := sql.InMemory()

	nodeID := types.NodeID{Key: "1", VRFPublicKey: []byte("1")}

	atx := newAtx(nodeID, types.NewLayerID(uint32(1)))

	require.NoError(t, Add(db, atx, time.Now()))
	buf, err := GetBlob(db, atx.ID())
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

	strIdx := strconv.Itoa(1)
	nodeID := types.NodeID{Key: strIdx, VRFPublicKey: []byte(strIdx)}
	atx := newAtx(nodeID, types.NewLayerID(uint32(1)))

	require.NoError(t, Add(db, atx, time.Now()))
	require.ErrorIs(t, Add(db, atx, time.Now()), sql.ErrObjectExists)

	got, err := Get(db, atx.ID())
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

func newAtx(nodeID types.NodeID, layerID types.LayerID) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPostChallenge: types.NIPostChallenge{
					NodeID:     nodeID,
					PubLayerID: layerID,
					StartTick:  0,
					EndTick:    1,
				},
				NumUnits: 2,
			},
		},
	}
	activationTx.CalcAndSetID()
	return activationTx
}
