package atxs_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const layersPerEpoch = 5

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func TestGetATXByID(t *testing.T) {
	db := sql.InMemory()

	atxList := make([]*types.VerifiedActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, err := newAtx(sig, types.LayerID(uint32(i)))
		require.NoError(t, err)
		atxList = append(atxList, atx)
	}

	for _, atx := range atxList {
		require.NoError(t, atxs.Add(db, atx))
	}

	for _, want := range atxList {
		got, err := atxs.Get(db, want.ID())
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	_, err := atxs.Get(db, types.ATXID(types.CalcHash32([]byte("0"))))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestHasID(t *testing.T) {
	db := sql.InMemory()

	atxList := make([]*types.VerifiedActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, err := newAtx(sig, types.LayerID(uint32(i)))
		require.NoError(t, err)
		atxList = append(atxList, atx)
	}

	for _, atx := range atxList {
		require.NoError(t, atxs.Add(db, atx))
	}

	for _, atx := range atxList {
		has, err := atxs.Has(db, atx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}

	has, err := atxs.Has(db, types.ATXID(types.CalcHash32([]byte("0"))))
	require.NoError(t, err)
	require.False(t, has)
}

func TestGetFirstIDByNodeID(t *testing.T) {
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Arrange

	atx1, err := newAtx(sig1, types.LayerID(uint32(1*layersPerEpoch)))
	require.NoError(t, err)

	atx2, err := newAtx(sig1, types.LayerID(uint32(2*layersPerEpoch)))
	require.NoError(t, err)
	atx2.Sequence = atx1.Sequence + 1
	atx2.Signature = sig1.Sign(signing.ATX, atx2.SignedBytes())

	atx3, err := newAtx(sig2, types.LayerID(uint32(3*layersPerEpoch)))
	require.NoError(t, err)

	atx4, err := newAtx(sig2, types.LayerID(uint32(4*layersPerEpoch)))
	require.NoError(t, err)
	atx4.Sequence = atx3.Sequence + 1
	atx4.Signature = sig2.Sign(signing.ATX, atx4.SignedBytes())

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
	}

	// Act & Assert

	id1, err := atxs.GetFirstIDByNodeID(db, sig1.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx1.ID(), id1)

	id2, err := atxs.GetFirstIDByNodeID(db, sig2.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx3.ID(), id2)

	_, err = atxs.GetLastIDByNodeID(db, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestGetByEpochAndNodeID(t *testing.T) {
	db := sql.InMemory()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1, err := newAtx(sig1, types.LayerID(uint32(1*layersPerEpoch)))
	require.NoError(t, err)

	atx2, err := newAtx(sig2, types.LayerID(uint32(2*layersPerEpoch)))
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2} {
		require.NoError(t, atxs.Add(db, atx))
	}

	// Act & Assert

	got, err := atxs.GetByEpochAndNodeID(db, types.EpochID(1), sig1.NodeID())
	require.NoError(t, err)
	require.Equal(t, atx1, got)

	got, err = atxs.GetByEpochAndNodeID(db, types.EpochID(2), sig1.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)

	got, err = atxs.GetByEpochAndNodeID(db, types.EpochID(1), sig2.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)

	got, err = atxs.GetByEpochAndNodeID(db, types.EpochID(2), sig2.NodeID())
	require.NoError(t, err)
	require.Equal(t, atx2, got)
}

func TestGetLastIDByNodeID(t *testing.T) {
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Arrange

	atx1, err := newAtx(sig1, types.LayerID(uint32(1*layersPerEpoch)))
	require.NoError(t, err)

	atx2, err := newAtx(sig1, types.LayerID(uint32(2*layersPerEpoch)))
	require.NoError(t, err)
	atx2.Sequence = atx1.Sequence + 1
	atx2.Signature = sig1.Sign(signing.ATX, atx2.SignedBytes())

	atx3, err := newAtx(sig2, types.LayerID(uint32(3*layersPerEpoch)))
	require.NoError(t, err)

	atx4, err := newAtx(sig2, types.LayerID(uint32(3*layersPerEpoch)))
	require.NoError(t, err)
	atx4.Sequence = atx3.Sequence + 1
	atx4.Signature = sig2.Sign(signing.ATX, atx4.SignedBytes())

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
	}

	// Act & Assert

	id1, err := atxs.GetLastIDByNodeID(db, sig1.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), id1)

	id2, err := atxs.GetLastIDByNodeID(db, sig2.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx4.ID(), id2)

	_, err = atxs.GetLastIDByNodeID(db, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestGetIDByEpochAndNodeID(t *testing.T) {
	db := sql.InMemory()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	l1 := types.LayerID(uint32(1 * layersPerEpoch))
	l2 := types.LayerID(uint32(2 * layersPerEpoch))
	l3 := types.LayerID(uint32(3 * layersPerEpoch))

	atx1, err := newAtx(sig1, l1)
	require.NoError(t, err)
	atx2, err := newAtx(sig1, l2)
	require.NoError(t, err)
	atx3, err := newAtx(sig2, l2)
	require.NoError(t, err)
	atx4, err := newAtx(sig2, l3)
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
	}

	l1n1, err := atxs.GetIDByEpochAndNodeID(db, l1.GetEpoch(), sig1.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx1.ID(), l1n1)

	_, err = atxs.GetIDByEpochAndNodeID(db, l1.GetEpoch(), sig2.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	l2n1, err := atxs.GetIDByEpochAndNodeID(db, l2.GetEpoch(), sig1.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), l2n1)

	l2n2, err := atxs.GetIDByEpochAndNodeID(db, l2.GetEpoch(), sig2.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx3.ID(), l2n2)

	_, err = atxs.GetIDByEpochAndNodeID(db, l3.GetEpoch(), sig1.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	l3n2, err := atxs.GetIDByEpochAndNodeID(db, l3.GetEpoch(), sig2.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx4.ID(), l3n2)
}

func TestGetIDsByEpoch(t *testing.T) {
	db := sql.InMemory()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	l1 := types.LayerID(uint32(1 * layersPerEpoch))
	l2 := types.LayerID(uint32(2 * layersPerEpoch))
	l3 := types.LayerID(uint32(3 * layersPerEpoch))

	atx1, err := newAtx(sig1, l1)
	require.NoError(t, err)
	atx2, err := newAtx(sig1, l2)
	require.NoError(t, err)
	atx3, err := newAtx(sig2, l2)
	require.NoError(t, err)
	atx4, err := newAtx(sig2, l3)
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
	}

	ids1, err := atxs.GetIDsByEpoch(db, l1.GetEpoch())
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx1.ID()}, ids1)

	ids2, err := atxs.GetIDsByEpoch(db, l2.GetEpoch())
	require.NoError(t, err)
	require.Contains(t, ids2, atx2.ID())
	require.Contains(t, ids2, atx3.ID())

	ids3, err := atxs.GetIDsByEpoch(db, l3.GetEpoch())
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx4.ID()}, ids3)
}

func TestVRFNonce(t *testing.T) {
	// Arrange
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce1 := types.VRFPostIndex(333)
	atx1, err := newAtx(sig, types.EpochID(20).FirstLayer())
	require.NoError(t, err)
	atx1.VRFNonce = &nonce1
	require.NoError(t, atxs.Add(db, atx1))

	atx2, err := newAtx(sig, types.EpochID(30).FirstLayer())
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx2))

	nonce3 := types.VRFPostIndex(777)
	atx3, err := newAtx(sig, types.EpochID(50).FirstLayer())
	require.NoError(t, err)
	atx3.VRFNonce = &nonce3
	require.NoError(t, atxs.Add(db, atx3))

	// Act & Assert

	// same epoch returns same nonce
	got, err := atxs.VRFNonce(db, sig.NodeID(), atx1.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	got, err = atxs.VRFNonce(db, sig.NodeID(), atx3.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce3, got)

	// between epochs returns previous nonce
	got, err = atxs.VRFNonce(db, sig.NodeID(), atx2.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	// later epoch returns newer nonce
	got, err = atxs.VRFNonce(db, sig.NodeID(), atx3.TargetEpoch()+10)
	require.NoError(t, err)
	require.Equal(t, nonce3, got)

	// before first epoch returns error
	_, err = atxs.VRFNonce(db, sig.NodeID(), atx1.TargetEpoch()-10)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestGetBlob(t *testing.T) {
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, types.LayerID(uint32(1)))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx))
	buf, err := atxs.GetBlob(db, atx.ID().Bytes())
	require.NoError(t, err)
	encoded, err := codec.Encode(atx.ActivationTx)
	require.NoError(t, err)
	require.Equal(t, encoded, buf)
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	nonExistingATXID := types.ATXID(types.CalcHash32([]byte("0")))
	_, err := atxs.Get(db, nonExistingATXID)
	require.ErrorIs(t, err, sql.ErrNotFound)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, types.LayerID(uint32(1)))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx))
	require.ErrorIs(t, atxs.Add(db, atx), sql.ErrObjectExists)

	got, err := atxs.Get(db, atx.ID())
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

func newAtx(signer *signing.EdSigner, layerID types.LayerID) (*types.VerifiedActivationTx, error) {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: layerID,
				PrevATXID:  types.RandomATXID(),
			},
			NumUnits: 2,
		},
	}

	activation.SignAndFinalizeAtx(signer, atx)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx.Verify(0, 1)
}

func TestPositioningID(t *testing.T) {
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
						NIPostChallenge: types.NIPostChallenge{
							PubLayerID: atx.epoch.FirstLayer(),
						},
						Coinbase: atx.coinbase,
						NumUnits: 2,
					},
				}

				sig, err := signing.NewEdSigner()
				require.NoError(t, err)
				require.NoError(t, activation.SignAndFinalizeAtx(sig, full))

				full.SetEffectiveNumUnits(full.NumUnits)
				full.SetReceived(time.Now())
				vAtx, err := full.Verify(atx.base, atx.count)
				require.NoError(t, err)

				require.NoError(t, atxs.Add(db, vAtx))
				ids = append(ids, full.ID())
			}
			rst, err := atxs.GetAtxIDWithMaxHeight(db)
			if len(tc.atxs) == 0 {
				require.ErrorIs(t, err, sql.ErrNotFound)
			} else {
				require.Equal(t, ids[tc.expect], rst)
			}
		})
	}
}
