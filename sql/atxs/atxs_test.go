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

func TestGet(t *testing.T) {
	db := sql.InMemory()

	atxList := make([]*types.VerifiedActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, err := newAtx(sig, withPublishEpoch(types.EpochID(i)))
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

func TestAll(t *testing.T) {
	db := sql.InMemory()

	atxList := make([]*types.VerifiedActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, err := newAtx(sig, withPublishEpoch(types.EpochID(i)))
		require.NoError(t, err)
		atxList = append(atxList, atx)
	}

	var expected []types.ATXID
	for _, atx := range atxList {
		require.NoError(t, atxs.Add(db, atx))
		expected = append(expected, atx.ID())
	}

	all, err := atxs.All(db)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, all)
}

func TestHasID(t *testing.T) {
	db := sql.InMemory()

	atxList := make([]*types.VerifiedActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, err := newAtx(sig, withPublishEpoch(types.EpochID(i)))
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

	atx1, err := newAtx(sig1, withPublishEpoch(1))
	require.NoError(t, err)

	atx2, err := newAtx(sig1, withPublishEpoch(2))
	require.NoError(t, err)
	atx2.Sequence = atx1.Sequence + 1
	atx2.Signature = sig1.Sign(signing.ATX, atx2.SignedBytes())

	atx3, err := newAtx(sig2, withPublishEpoch(3))
	require.NoError(t, err)

	atx4, err := newAtx(sig2, withPublishEpoch(4))
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

func TestLatestN(t *testing.T) {
	db := sql.InMemory()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1, err := newAtx(sig1, withPublishEpoch(1), withSequence(0))
	require.NoError(t, err)
	atx2, err := newAtx(sig1, withPublishEpoch(2), withSequence(1))
	require.NoError(t, err)
	atx3, err := newAtx(sig2, withPublishEpoch(3), withSequence(1))
	require.NoError(t, err)
	atx4, err := newAtx(sig2, withPublishEpoch(4), withSequence(2))
	require.NoError(t, err)
	atx5, err := newAtx(sig2, withPublishEpoch(5), withSequence(3))
	require.NoError(t, err)
	atx6, err := newAtx(sig3, withPublishEpoch(1), withSequence(0))
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4, atx5, atx6} {
		require.NoError(t, atxs.Add(db, atx))
	}

	for _, tc := range []struct {
		desc     string
		n        int
		expected map[types.NodeID]map[types.ATXID]struct{}
	}{
		{
			desc: "latest 3",
			n:    3,
			expected: map[types.NodeID]map[types.ATXID]struct{}{
				sig1.NodeID(): {
					atx1.ID(): struct{}{},
					atx2.ID(): struct{}{},
				},
				sig2.NodeID(): {
					atx3.ID(): struct{}{},
					atx4.ID(): struct{}{},
					atx5.ID(): struct{}{},
				},
				sig3.NodeID(): {
					atx6.ID(): struct{}{},
				},
			},
		},
		{
			desc: "latest 2",
			n:    2,
			expected: map[types.NodeID]map[types.ATXID]struct{}{
				sig1.NodeID(): {
					atx1.ID(): struct{}{},
					atx2.ID(): struct{}{},
				},
				sig2.NodeID(): {
					atx4.ID(): struct{}{},
					atx5.ID(): struct{}{},
				},
				sig3.NodeID(): {
					atx6.ID(): struct{}{},
				},
			},
		},
		{
			desc: "latest 1",
			n:    1,
			expected: map[types.NodeID]map[types.ATXID]struct{}{
				sig1.NodeID(): {
					atx2.ID(): struct{}{},
				},
				sig2.NodeID(): {
					atx5.ID(): struct{}{},
				},
				sig3.NodeID(): {
					atx6.ID(): struct{}{},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := atxs.LatestN(db, tc.n)
			require.NoError(t, err)
			for _, catx := range got {
				delete(tc.expected[catx.SmesherID], catx.ID)
				if len(tc.expected[catx.SmesherID]) == 0 {
					delete(tc.expected, catx.SmesherID)
				}
			}
			require.Empty(t, tc.expected)
		})
	}
}

func TestGetByEpochAndNodeID(t *testing.T) {
	db := sql.InMemory()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1, err := newAtx(sig1, withPublishEpoch(1))
	require.NoError(t, err)

	atx2, err := newAtx(sig2, withPublishEpoch(2))
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

	atx1, err := newAtx(sig1, withPublishEpoch(1))
	require.NoError(t, err)

	atx2, err := newAtx(sig1, withPublishEpoch(2))
	require.NoError(t, err)
	atx2.Sequence = atx1.Sequence + 1
	atx2.Signature = sig1.Sign(signing.ATX, atx2.SignedBytes())

	atx3, err := newAtx(sig2, withPublishEpoch(3))
	require.NoError(t, err)

	atx4, err := newAtx(sig2, withPublishEpoch(3))
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

	e1 := types.EpochID(1)
	e2 := types.EpochID(2)
	e3 := types.EpochID(3)

	atx1, err := newAtx(sig1, withPublishEpoch(e1))
	require.NoError(t, err)
	atx2, err := newAtx(sig1, withPublishEpoch(e2))
	require.NoError(t, err)
	atx3, err := newAtx(sig2, withPublishEpoch(e2))
	require.NoError(t, err)
	atx4, err := newAtx(sig2, withPublishEpoch(e3))
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
	}

	l1n1, err := atxs.GetIDByEpochAndNodeID(db, e1, sig1.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx1.ID(), l1n1)

	_, err = atxs.GetIDByEpochAndNodeID(db, e1, sig2.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	l2n1, err := atxs.GetIDByEpochAndNodeID(db, e2, sig1.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx2.ID(), l2n1)

	l2n2, err := atxs.GetIDByEpochAndNodeID(db, e2, sig2.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx3.ID(), l2n2)

	_, err = atxs.GetIDByEpochAndNodeID(db, e3, sig1.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	l3n2, err := atxs.GetIDByEpochAndNodeID(db, e3, sig2.NodeID())
	require.NoError(t, err)
	require.EqualValues(t, atx4.ID(), l3n2)
}

func TestGetIDsByEpoch(t *testing.T) {
	db := sql.InMemory()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	e1 := types.EpochID(1)
	e2 := types.EpochID(2)
	e3 := types.EpochID(3)

	atx1, err := newAtx(sig1, withPublishEpoch(e1))
	require.NoError(t, err)
	atx2, err := newAtx(sig1, withPublishEpoch(e2))
	require.NoError(t, err)
	atx3, err := newAtx(sig2, withPublishEpoch(e2))
	require.NoError(t, err)
	atx4, err := newAtx(sig2, withPublishEpoch(e3))
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
	}

	ids1, err := atxs.GetIDsByEpoch(db, e1)
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx1.ID()}, ids1)

	ids2, err := atxs.GetIDsByEpoch(db, e2)
	require.NoError(t, err)
	require.Contains(t, ids2, atx2.ID())
	require.Contains(t, ids2, atx3.ID())

	ids3, err := atxs.GetIDsByEpoch(db, e3)
	require.NoError(t, err)
	require.EqualValues(t, []types.ATXID{atx4.ID()}, ids3)
}

func TestVRFNonce(t *testing.T) {
	// Arrange
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce1 := types.VRFPostIndex(333)
	atx1, err := newAtx(sig, withPublishEpoch(types.EpochID(20)))
	require.NoError(t, err)
	atx1.VRFNonce = &nonce1
	require.NoError(t, atxs.Add(db, atx1))

	atx2, err := newAtx(sig, withPublishEpoch(types.EpochID(30)))
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx2))

	nonce3 := types.VRFPostIndex(777)
	atx3, err := newAtx(sig, withPublishEpoch(types.EpochID(50)))
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
	atx, err := newAtx(sig, withPublishEpoch(1))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx))
	buf, err := atxs.GetBlob(db, atx.ID().Bytes())
	require.NoError(t, err)
	encoded, err := codec.Encode(atx.ActivationTx)
	require.NoError(t, err)
	require.Equal(t, encoded, buf)
}

func TestCheckpointATX(t *testing.T) {
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, withPublishEpoch(3), withSequence(4))
	require.NoError(t, err)
	catx := &atxs.CheckpointAtx{
		ID:             atx.ID(),
		Epoch:          atx.PublishEpoch,
		CommitmentATX:  types.ATXID{1, 2, 3},
		VRFNonce:       types.VRFPostIndex(119),
		NumUnits:       atx.NumUnits,
		BaseTickHeight: 1000,
		TickCount:      atx.TickCount() + 1,
		SmesherID:      sig.NodeID(),
		Sequence:       atx.Sequence + 1,
		Coinbase:       types.Address{3, 2, 1},
	}
	require.NoError(t, atxs.AddCheckpointed(db, catx))
	got, err := atxs.Get(db, catx.ID)
	require.NoError(t, err)
	require.Equal(t, catx.ID, got.ID())
	require.Equal(t, catx.Epoch, got.PublishEpoch)
	require.Equal(t, catx.NumUnits, got.NumUnits)
	require.Equal(t, catx.BaseTickHeight, got.BaseTickHeight())
	require.Equal(t, catx.TickCount, got.TickCount())
	require.Equal(t, catx.SmesherID, got.SmesherID)
	require.Equal(t, catx.Sequence, got.Sequence)
	require.Equal(t, catx.Coinbase, got.Coinbase)
	require.True(t, got.Received().IsZero(), got.Received())
	require.True(t, got.Golden())

	gotcommit, err := atxs.CommitmentATX(db, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, catx.CommitmentATX, gotcommit)
	gotvrf, err := atxs.VRFNonce(db, sig.NodeID(), atx.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, catx.VRFNonce, gotvrf)

	// checkpoint atx does not have actual atx data
	blob, err := atxs.GetBlob(db, catx.ID.Bytes())
	require.NoError(t, err)
	require.Nil(t, blob)
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	nonExistingATXID := types.ATXID(types.CalcHash32([]byte("0")))
	_, err := atxs.Get(db, nonExistingATXID)
	require.ErrorIs(t, err, sql.ErrNotFound)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, withPublishEpoch(1))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx))
	require.ErrorIs(t, atxs.Add(db, atx), sql.ErrObjectExists)

	got, err := atxs.Get(db, atx.ID())
	require.NoError(t, err)
	require.Equal(t, atx, got)
}

type createAtxOpt func(*types.ActivationTx)

func withPublishEpoch(epoch types.EpochID) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.PublishEpoch = epoch
	}
}

func withSequence(seq uint64) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.Sequence = seq
	}
}

func newAtx(signer *signing.EdSigner, opts ...createAtxOpt) (*types.VerifiedActivationTx, error) {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PrevATXID: types.RandomATXID(),
			},
			Coinbase: types.Address{1, 2, 3},
			NumUnits: 2,
		},
	}
	for _, opt := range opts {
		opt(atx)
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
							PublishEpoch: atx.epoch,
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

func TestLatest(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		epochs []uint32
		expect uint32
	}{
		{"empty", nil, 0},
		{"in order", []uint32{1, 2, 3, 4}, 4},
		{"out of order", []uint32{3, 4, 1, 2}, 4},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			for i, epoch := range tc.epochs {
				full := &types.ActivationTx{
					InnerActivationTx: types.InnerActivationTx{
						NIPostChallenge: types.NIPostChallenge{
							PublishEpoch: types.EpochID(epoch),
						},
					},
				}
				full.SetEffectiveNumUnits(1)
				full.SetReceived(time.Now())
				full.SetID(types.ATXID{byte(i)})
				vAtx, err := full.Verify(0, 1)
				require.NoError(t, err)
				require.NoError(t, atxs.Add(db, vAtx))
			}
			latest, err := atxs.LatestEpoch(db)
			require.NoError(t, err)
			require.EqualValues(t, tc.expect, latest)
		})
	}
}
