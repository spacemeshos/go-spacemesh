package atxs_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
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
	atx2.Signature = sig1.Sign(signing.ATX, wire.ActivationTxToWireV1(atx2.ActivationTx).SignedBytes())

	atx3, err := newAtx(sig2, withPublishEpoch(3))
	require.NoError(t, err)

	atx4, err := newAtx(sig2, withPublishEpoch(4))
	require.NoError(t, err)
	atx4.Sequence = atx3.Sequence + 1
	atx4.Signature = sig2.Sign(signing.ATX, wire.ActivationTxToWireV1(atx4.ActivationTx).SignedBytes())

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
	atx2.Signature = sig1.Sign(signing.ATX, wire.ActivationTxToWireV1(atx2.ActivationTx).SignedBytes())

	atx3, err := newAtx(sig2, withPublishEpoch(3))
	require.NoError(t, err)

	atx4, err := newAtx(sig2, withPublishEpoch(4))
	require.NoError(t, err)
	atx4.Sequence = atx3.Sequence + 1
	atx4.Signature = sig2.Sign(signing.ATX, wire.ActivationTxToWireV1(atx4.ActivationTx).SignedBytes())

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
	ctx := context.Background()

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

	ids1, err := atxs.GetIDsByEpoch(ctx, db, e1)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.ATXID{atx1.ID()}, ids1)

	ids2, err := atxs.GetIDsByEpoch(ctx, db, e2)
	require.NoError(t, err)
	require.Contains(t, ids2, atx2.ID())
	require.Contains(t, ids2, atx3.ID())

	ids3, err := atxs.GetIDsByEpoch(ctx, db, e3)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.ATXID{atx4.ID()}, ids3)
}

func TestGetIDsByEpochCached(t *testing.T) {
	db := sql.InMemory(sql.WithQueryCache(true))
	ctx := context.Background()

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
	atx5, err := newAtx(sig2, withPublishEpoch(e3))
	require.NoError(t, err)
	atx6, err := newAtx(sig2, withPublishEpoch(e3))
	require.NoError(t, err)

	for _, atx := range []*types.VerifiedActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx))
		atxs.AtxAdded(db, atx)
	}

	// insert atx + insert blob for each ATX
	require.Equal(t, 8, db.QueryCount())

	for i := 0; i < 3; i++ {
		ids1, err := atxs.GetIDsByEpoch(ctx, db, e1)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.ATXID{atx1.ID()}, ids1)
		require.Equal(t, 9, db.QueryCount())
	}

	for i := 0; i < 3; i++ {
		ids2, err := atxs.GetIDsByEpoch(ctx, db, e2)
		require.NoError(t, err)
		require.Contains(t, ids2, atx2.ID())
		require.Contains(t, ids2, atx3.ID())
		require.Equal(t, 10, db.QueryCount())
	}

	for i := 0; i < 3; i++ {
		ids3, err := atxs.GetIDsByEpoch(ctx, db, e3)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.ATXID{atx4.ID()}, ids3)
		require.Equal(t, 11, db.QueryCount())
	}

	require.NoError(t, db.WithTx(context.Background(), func(tx *sql.Tx) error {
		atxs.Add(tx, atx5)
		return nil
	}))
	atxs.AtxAdded(db, atx5)
	require.Equal(t, 13, db.QueryCount())

	ids3, err := atxs.GetIDsByEpoch(ctx, db, e3)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.ATXID{atx4.ID(), atx5.ID()}, ids3)
	require.Equal(t, 13, db.QueryCount()) // not incremented after Add

	require.Error(t, db.WithTx(context.Background(), func(tx *sql.Tx) error {
		atxs.Add(tx, atx6)
		return errors.New("fail") // rollback
	}))

	// atx6 should not be in the cache
	ids4, err := atxs.GetIDsByEpoch(ctx, db, e3)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.ATXID{atx4.ID(), atx5.ID()}, ids4)
	require.Equal(t, 16, db.QueryCount()) // not incremented after Add
}

func TestVRFNonce(t *testing.T) {
	// Arrange
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce1 := types.VRFPostIndex(333)
	atx1, err := newAtx(sig, withPublishEpoch(types.EpochID(20)), withNonce(nonce1))
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx1))

	atx2, err := newAtx(sig, withPublishEpoch(types.EpochID(30)), withNoNonce(),
		withPrevATXID(atx1.ID()))
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx2))

	nonce3 := types.VRFPostIndex(777)
	atx3, err := newAtx(sig, withPublishEpoch(types.EpochID(50)), withNonce(nonce3),
		withPrevATXID(atx2.ID()))
	require.NoError(t, err)
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

func TestLoadBlob(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx1, err := newAtx(sig, withPublishEpoch(1))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx1))

	var blob1 sql.Blob
	require.NoError(t, atxs.LoadBlob(ctx, db, atx1.ID().Bytes(), &blob1))
	encoded := codec.MustEncode(wire.ActivationTxToWireV1(atx1.ActivationTx))
	require.Equal(t, encoded, blob1.Bytes)

	blobSizes, err := atxs.GetBlobSizes(db, [][]byte{atx1.ID().Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes)}, blobSizes)

	var blob2 sql.Blob
	atx2, err := newAtx(sig, withPublishEpoch(1))
	nodeID := types.RandomNodeID()
	atx2.NodeID = &nodeID // ensure ATXs differ in size
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx2))
	require.NoError(t, atxs.LoadBlob(ctx, db, atx2.ID().Bytes(), &blob2))
	encoded = codec.MustEncode(wire.ActivationTxToWireV1(atx2.ActivationTx))
	require.Equal(t, encoded, blob2.Bytes)
	blobSizes, err = atxs.GetBlobSizes(db, [][]byte{
		atx1.ID().Bytes(),
		atx2.ID().Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes)}, blobSizes)

	noSuchID := types.RandomATXID()
	require.ErrorIs(t, atxs.LoadBlob(ctx, db, noSuchID[:], &sql.Blob{}), sql.ErrNotFound)

	blobSizes, err = atxs.GetBlobSizes(db, [][]byte{
		atx1.ID().Bytes(),
		noSuchID.Bytes(),
		atx2.ID().Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), -1, len(blob2.Bytes)}, blobSizes)
}

func TestGetBlobCached(t *testing.T) {
	db := sql.InMemory(sql.WithQueryCache(true))
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, withPublishEpoch(1))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx))
	encoded, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
	require.NoError(t, err)
	require.Equal(t, 2, db.QueryCount()) // insert atx + blob

	for i := 0; i < 3; i++ {
		var b sql.Blob
		require.NoError(t, atxs.LoadBlob(ctx, db, atx.ID().Bytes(), &b))
		require.Equal(t, encoded, b.Bytes)
		require.Equal(t, 3, db.QueryCount())
	}
}

func TestCachedBlobEviction(t *testing.T) {
	db := sql.InMemory(
		sql.WithQueryCache(true),
		sql.WithQueryCacheSizes(map[sql.QueryCacheKind]int{
			atxs.CacheKindATXBlob: 10,
		}))
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	addedATXs := make([]*types.VerifiedActivationTx, 11)
	blobs := make([][]byte, 11)
	var b sql.Blob
	for n := range addedATXs {
		atx, err := newAtx(sig, withPublishEpoch(1))
		require.NoError(t, err)
		require.NoError(t, atxs.Add(db, atx))
		addedATXs[n] = atx
		encoded, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
		require.NoError(t, err)
		blobs[n] = encoded
		require.NoError(t, atxs.LoadBlob(ctx, db, atx.ID().Bytes(), &b))
		require.Equal(t, encoded, b.Bytes)
	}

	// insert atx + insert blob + load blob each time
	require.Equal(t, 33, db.QueryCount())

	// The ATXs except the first one stay in place
	for n, atx := range addedATXs[1:] {
		require.NoError(t, atxs.LoadBlob(ctx, db, atx.ID().Bytes(), &b))
		require.Equal(t, blobs[n+1], b.Bytes)
		require.Equal(t, 33, db.QueryCount())
	}

	// The first ATX is evicted. We check it after the loop to avoid additional evictions.
	require.NoError(t, atxs.LoadBlob(ctx, db, addedATXs[0].ID().Bytes(), &b))
	require.Equal(t, blobs[0], b.Bytes)
	require.Equal(t, 34, db.QueryCount())
}

func TestCheckpointATX(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

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
	var blob sql.Blob
	require.NoError(t, atxs.LoadBlob(ctx, db, catx.ID.Bytes(), &blob))
	require.Empty(t, blob.Bytes)
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

func withNonce(nonce types.VRFPostIndex) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.VRFNonce = &nonce
	}
}

func withNoNonce() createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.VRFNonce = nil
	}
}

func withPrevATXID(id types.ATXID) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.PrevATXID = id
	}
}

func newAtx(signer *signing.EdSigner, opts ...createAtxOpt) (*types.VerifiedActivationTx, error) {
	nonce := types.VRFPostIndex(42)
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PrevATXID: types.RandomATXID(),
			},
			Coinbase: types.Address{1, 2, 3},
			NumUnits: 2,
			VRFNonce: &nonce,
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

type header struct {
	coinbase    types.Address
	base, count uint64
	epoch       types.EpochID
	malicious   bool
	filteredOut bool
}

func createAtx(tb testing.TB, db *sql.Database, hdr header) (types.ATXID, *signing.EdSigner) {
	full := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: hdr.epoch,
			},
			Coinbase: hdr.coinbase,
			NumUnits: 2,
		},
	}
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)

	require.NoError(tb, activation.SignAndFinalizeAtx(sig, full))

	full.SetEffectiveNumUnits(full.NumUnits)
	full.SetReceived(time.Now())
	vAtx, err := full.Verify(hdr.base, hdr.count)
	require.NoError(tb, err)

	require.NoError(tb, atxs.Add(db, vAtx))
	if hdr.malicious {
		require.NoError(tb, identities.SetMalicious(db, sig.NodeID(), []byte("bad"), time.Now()))
	}

	return full.ID(), sig
}

func TestGetIDWithMaxHeight(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		atxs   []header
		pref   int
		expect int
	}{
		{
			desc: "not found",
		},
		{
			desc: "by epoch",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 1, epoch: 1},
				{coinbase: types.Address{2}, base: 1, count: 2, epoch: 2},
			},
			expect: 1,
			pref:   -1,
		},
		{
			desc: "highest in prev epoch",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 3, epoch: 1}, // too old
				{coinbase: types.Address{2}, base: 1, count: 2, epoch: 2},
				{coinbase: types.Address{3}, base: 1, count: 1, epoch: 3},
			},
			pref:   -1,
			expect: 1,
		},
		{
			desc: "prefer later epoch",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1},
				{coinbase: types.Address{2}, base: 1, count: 2, epoch: 2},
			},
			pref:   -1,
			expect: 1,
		},
		{
			desc: "prefer node id",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1},
				{coinbase: types.Address{2}, base: 1, count: 2, epoch: 1},
				{coinbase: types.Address{3}, base: 1, count: 2, epoch: 2},
			},
			pref:   1,
			expect: 1,
		},
		{
			desc: "skip malicious id",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1, malicious: true},
				{coinbase: types.Address{2}, base: 1, count: 2, epoch: 1, malicious: true},
				{coinbase: types.Address{3}, base: 1, count: 1, epoch: 2},
			},
			pref:   1,
			expect: 2,
		},
		{
			desc: "skip malicious id not found",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1, malicious: true},
				{coinbase: types.Address{2}, base: 1, count: 2, epoch: 1, malicious: true},
				{coinbase: types.Address{3}, base: 1, count: 2, epoch: 2, malicious: true},
			},
			pref:   1,
			expect: -1,
		},
		{
			desc: "by tick height",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 2, epoch: 1},
				{coinbase: types.Address{2}, base: 1, count: 1, epoch: 1},
			},
			pref:   -1,
			expect: 0,
		},
		{
			desc: "by filter",
			atxs: []header{
				{coinbase: types.Address{1}, base: 1, count: 30, epoch: 3, filteredOut: true},
				{coinbase: types.Address{2}, base: 1, count: 20, epoch: 3, filteredOut: true},
				{coinbase: types.Address{3}, base: 1, count: 10, epoch: 2, filteredOut: true},
				{coinbase: types.Address{4}, base: 1, count: 1, epoch: 2},
				{coinbase: types.Address{5}, base: 1, count: 100, epoch: 1},
			},
			pref:   -1,
			expect: 3,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			var sigs []*signing.EdSigner
			var ids []types.ATXID
			filtered := make(map[types.ATXID]struct{})

			for _, atx := range tc.atxs {
				id, sig := createAtx(t, db, atx)
				ids = append(ids, id)
				sigs = append(sigs, sig)
				if atx.filteredOut {
					filtered[id] = struct{}{}
				}
			}
			var pref types.NodeID
			if tc.pref > 0 {
				pref = sigs[tc.pref].NodeID()
			}
			rst, err := atxs.GetIDWithMaxHeight(db, pref, func(id types.ATXID) bool {
				_, ok := filtered[id]
				return !ok
			})
			if len(tc.atxs) == 0 || tc.expect < 0 {
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

func Test_PrevATXCollisions(t *testing.T) {
	db := sql.InMemory()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// create two ATXs with the same PrevATXID
	prevATXID := types.RandomATXID()

	atx1, err := newAtx(sig, withPublishEpoch(1), withPrevATXID(prevATXID))
	require.NoError(t, err)
	atx2, err := newAtx(sig, withPublishEpoch(2), withPrevATXID(prevATXID))
	require.NoError(t, err)

	require.NoError(t, atxs.Add(db, atx1))
	require.NoError(t, atxs.Add(db, atx2))

	// verify that the ATXs were added
	got1, err := atxs.Get(db, atx1.ID())
	require.NoError(t, err)
	require.Equal(t, atx1, got1)

	got2, err := atxs.Get(db, atx2.ID())
	require.NoError(t, err)
	require.Equal(t, atx2, got2)

	// add 10 valid ATXs by 10 other smeshers
	atxMap := make(map[types.NodeID][]*types.VerifiedActivationTx)
	for i := 2; i < 12; i++ {
		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		if len(atxMap[otherSig.NodeID()]) == 0 {
			atx, err := newAtx(otherSig, withPublishEpoch(types.EpochID(i)))
			require.NoError(t, err)
			require.NoError(t, atxs.Add(db, atx))
		} else {
			atx, err := newAtx(otherSig, withPublishEpoch(types.EpochID(i)),
				withPrevATXID(atxMap[otherSig.NodeID()][len(atxMap[otherSig.NodeID()])-1].ID()),
			)
			require.NoError(t, err)
			require.NoError(t, atxs.Add(db, atx))
		}
	}

	// get the collisions
	got, err := atxs.PrevATXCollisions(db)
	require.NoError(t, err)
	require.Len(t, got, 1)

	require.Equal(t, sig.NodeID(), got[0].NodeID1)
	require.Equal(t, sig.NodeID(), got[0].NodeID2)
	require.ElementsMatch(t, []types.ATXID{atx1.ID(), atx2.ID()}, []types.ATXID{got[0].ATX1, got[0].ATX2})
}
