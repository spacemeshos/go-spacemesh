package atxs_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
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

	atxList := make([]*types.ActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, blob := newAtx(t, sig, withPublishEpoch(types.EpochID(i)))
		atxList = append(atxList, atx)
		require.NoError(t, atxs.Add(db, atx, blob))
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

	var expected []types.ATXID
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, blob := newAtx(t, sig, withPublishEpoch(types.EpochID(i)))
		require.NoError(t, atxs.Add(db, atx, blob))
		expected = append(expected, atx.ID())
	}

	all, err := atxs.All(db)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, all)
}

func TestHasID(t *testing.T) {
	db := sql.InMemory()

	atxList := make([]*types.ActivationTx, 0)
	for i := 0; i < 3; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, blob := newAtx(t, sig, withPublishEpoch(types.EpochID(i)))
		require.NoError(t, atxs.Add(db, atx, blob))
		atxList = append(atxList, atx)
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

func Test_IdentityExists(t *testing.T) {
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	yes, err := atxs.IdentityExists(db, sig.NodeID())
	require.NoError(t, err)
	require.False(t, yes)

	atx, blob := newAtx(t, sig)
	require.NoError(t, atxs.Add(db, atx, blob))

	yes, err = atxs.IdentityExists(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, yes)
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
	atx1, _ := newAtx(t, sig1, withPublishEpoch(1))
	atx2, _ := newAtx(t, sig1, withPublishEpoch(2), withSequence(atx1.Sequence+1))
	atx3, _ := newAtx(t, sig2, withPublishEpoch(3))
	atx4, _ := newAtx(t, sig2, withPublishEpoch(4), withSequence(atx3.Sequence+1))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
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

	atx1, _ := newAtx(t, sig1, withPublishEpoch(1), withSequence(0))
	atx2, _ := newAtx(t, sig1, withPublishEpoch(2), withSequence(1))
	atx3, _ := newAtx(t, sig2, withPublishEpoch(3), withSequence(1))
	atx4, _ := newAtx(t, sig2, withPublishEpoch(4), withSequence(2))
	atx5, _ := newAtx(t, sig2, withPublishEpoch(5), withSequence(3))
	atx6, _ := newAtx(t, sig3, withPublishEpoch(1), withSequence(0))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4, atx5, atx6} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
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

	atx1, _ := newAtx(t, sig1, withPublishEpoch(1))
	atx2, _ := newAtx(t, sig2, withPublishEpoch(2))

	for _, atx := range []*types.ActivationTx{atx1, atx2} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
	}

	// Act & Assert

	got, err := atxs.GetByEpochAndNodeID(db, types.EpochID(1), sig1.NodeID())
	require.NoError(t, err)
	require.Equal(t, atx1.ID(), got)

	_, err = atxs.GetByEpochAndNodeID(db, types.EpochID(2), sig1.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	_, err = atxs.GetByEpochAndNodeID(db, types.EpochID(1), sig2.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	got, err = atxs.GetByEpochAndNodeID(db, types.EpochID(2), sig2.NodeID())
	require.NoError(t, err)
	require.Equal(t, atx2.ID(), got)
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
	atx1, _ := newAtx(t, sig1, withPublishEpoch(1))
	atx2, _ := newAtx(t, sig1, withPublishEpoch(2), withSequence(atx1.Sequence+1))
	atx3, _ := newAtx(t, sig2, withPublishEpoch(3))
	atx4, _ := newAtx(t, sig2, withPublishEpoch(4), withSequence(atx3.Sequence+1))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
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

	atx1, _ := newAtx(t, sig1, withPublishEpoch(e1))
	atx2, _ := newAtx(t, sig1, withPublishEpoch(e2))
	atx3, _ := newAtx(t, sig2, withPublishEpoch(e2))
	atx4, _ := newAtx(t, sig2, withPublishEpoch(e3))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
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

	atx1, _ := newAtx(t, sig1, withPublishEpoch(e1))
	atx2, _ := newAtx(t, sig1, withPublishEpoch(e2))
	atx3, _ := newAtx(t, sig2, withPublishEpoch(e2))
	atx4, _ := newAtx(t, sig2, withPublishEpoch(e3))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
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

	atx1, _ := newAtx(t, sig1, withPublishEpoch(e1))
	atx2, _ := newAtx(t, sig1, withPublishEpoch(e2))
	atx3, _ := newAtx(t, sig2, withPublishEpoch(e2))
	atx4, _ := newAtx(t, sig2, withPublishEpoch(e3))
	atx5, _ := newAtx(t, sig2, withPublishEpoch(e3))
	atx6, _ := newAtx(t, sig2, withPublishEpoch(e3))

	for _, atx := range []*types.ActivationTx{atx1, atx2, atx3, atx4} {
		require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
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
		atxs.Add(tx, atx5, types.AtxBlob{})
		return nil
	}))
	atxs.AtxAdded(db, atx5)
	require.Equal(t, 13, db.QueryCount())

	ids3, err := atxs.GetIDsByEpoch(ctx, db, e3)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.ATXID{atx4.ID(), atx5.ID()}, ids3)
	require.Equal(t, 13, db.QueryCount()) // not incremented after Add

	require.Error(t, db.WithTx(context.Background(), func(tx *sql.Tx) error {
		atxs.Add(tx, atx6, types.AtxBlob{})
		return errors.New("fail") // rollback
	}))

	// atx6 should not be in the cache
	ids4, err := atxs.GetIDsByEpoch(ctx, db, e3)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.ATXID{atx4.ID(), atx5.ID()}, ids4)
	require.Equal(t, 16, db.QueryCount()) // not incremented after Add
}

func Test_IterateAtxsWithMalfeasance(t *testing.T) {
	db := sql.InMemory()

	e1 := types.EpochID(1)
	m := make(map[types.ATXID]bool)
	for i := uint32(0); i < 20; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, blob := newAtx(t, sig, withPublishEpoch(types.EpochID(i/4)))
		require.NoError(t, atxs.Add(db, atx, blob))
		malicious := (i % 2) == 0
		m[atx.ID()] = malicious
		if malicious {
			require.NoError(t, identities.SetMalicious(db, sig.NodeID(), []byte("bad"), time.Now()))
		}
	}

	n := 0
	err := atxs.IterateAtxsWithMalfeasance(db, e1, func(atx *types.ActivationTx, malicious bool) bool {
		require.Contains(t, m, atx.ID())
		require.Equal(t, m[atx.ID()], malicious)
		delete(m, atx.ID())
		n++
		return n < 2
	})
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Len(t, m, 20-2)
}

func Test_IterateAtxIdsWithMalfeasance(t *testing.T) {
	db := sql.InMemory()

	e1 := types.EpochID(1)
	m := make(map[types.ATXID]bool)
	for i := uint32(0); i < 20; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, blob := newAtx(t, sig, withPublishEpoch(types.EpochID(i/4)))
		require.NoError(t, atxs.Add(db, atx, blob))
		malicious := (i % 2) == 0
		m[atx.ID()] = malicious
		if malicious {
			require.NoError(t, identities.SetMalicious(db, sig.NodeID(), []byte("bad"), time.Now()))
		}
	}

	n := 0
	err := atxs.IterateAtxIdsWithMalfeasance(db, e1, func(id types.ATXID, malicious bool) bool {
		require.Contains(t, m, id)
		require.Equal(t, m[id], malicious)
		delete(m, id)
		n++
		return n < 2
	})
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Len(t, m, 20-2)
}

func TestVRFNonce(t *testing.T) {
	// Arrange
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1, blob := newAtx(t, sig, withPublishEpoch(20), withNonce(333))
	require.NoError(t, atxs.Add(db, atx1, blob))

	atx2, blob := newAtx(t, sig, withPublishEpoch(50), withNonce(777), withPrevATXID(atx1.ID()))
	require.NoError(t, atxs.Add(db, atx2, blob))

	// Act & Assert

	// same epoch returns same nonce
	got, err := atxs.VRFNonce(db, sig.NodeID(), atx1.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, atx1.VRFNonce, got)

	got, err = atxs.VRFNonce(db, sig.NodeID(), atx2.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, atx2.VRFNonce, got)

	// later epoch returns newer nonce
	got, err = atxs.VRFNonce(db, sig.NodeID(), atx2.TargetEpoch()+10)
	require.NoError(t, err)
	require.Equal(t, atx2.VRFNonce, got)

	// before first epoch returns error
	_, err = atxs.VRFNonce(db, sig.NodeID(), atx1.TargetEpoch()-10)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestLoadBlob(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx1, blob := newAtx(t, sig, withPublishEpoch(1))
	require.NoError(t, atxs.Add(db, atx1, blob))

	var blob1 sql.Blob
	version, err := atxs.LoadBlob(ctx, db, atx1.ID().Bytes(), &blob1)
	require.NoError(t, err)
	require.Equal(t, types.AtxV1, version)

	require.Equal(t, blob.Blob, blob1.Bytes)

	blobSizes, err := atxs.GetBlobSizes(db, [][]byte{atx1.ID().Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes)}, blobSizes)

	var blob2 sql.Blob
	atx2, blob := newAtx(t, sig)
	blob.Blob = []byte("blob2 of different size")
	blob.Version = types.AtxV2

	require.NoError(t, atxs.Add(db, atx2, blob))
	version, err = atxs.LoadBlob(ctx, db, atx2.ID().Bytes(), &blob2)
	require.NoError(t, err)
	require.Equal(t, blob.Version, version)
	require.Equal(t, blob.Blob, blob2.Bytes)

	blobSizes, err = atxs.GetBlobSizes(db, [][]byte{
		atx1.ID().Bytes(),
		atx2.ID().Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes)}, blobSizes)
	require.NotEqual(t, len(blob1.Bytes), len(blob2.Bytes))

	noSuchID := types.RandomATXID()
	_, err = atxs.LoadBlob(ctx, db, noSuchID[:], &sql.Blob{})
	require.ErrorIs(t, err, sql.ErrNotFound)

	blobSizes, err = atxs.GetBlobSizes(db, [][]byte{
		atx1.ID().Bytes(),
		noSuchID.Bytes(),
		atx2.ID().Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), -1, len(blob2.Bytes)}, blobSizes)
}

func TestLoadBlob_DefaultsToV1(t *testing.T) {
	db := sql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, blob := newAtx(t, sig)
	blob.Version = 0

	require.NoError(t, atxs.Add(db, atx, blob))

	var b sql.Blob
	version, err := atxs.LoadBlob(context.Background(), db, atx.ID().Bytes(), &b)
	require.NoError(t, err)
	require.Equal(t, types.AtxV1, version)
	require.Equal(t, blob.Blob, b.Bytes)
}

func TestGetBlobCached(t *testing.T) {
	db := sql.InMemory(sql.WithQueryCache(true))
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, blob := newAtx(t, sig, withPublishEpoch(1))

	require.NoError(t, atxs.Add(db, atx, blob))
	require.Equal(t, 2, db.QueryCount()) // insert atx + blob

	for i := 0; i < 3; i++ {
		var b sql.Blob
		_, err := atxs.LoadBlob(ctx, db, atx.ID().Bytes(), &b)
		require.NoError(t, err)
		require.Equal(t, blob.Blob, b.Bytes)
		require.Equal(t, 3, db.QueryCount())
	}
}

// Test that we don't put in the cache a reference to the blob that was passed to LoadBlob.
// Each cache entry must use a unique slice for the blob.
func TestGetBlobCached_CacheEntriesAreDistinct(t *testing.T) {
	db := sql.InMemory(sql.WithQueryCache(true))

	atx := types.ActivationTx{}
	atx.SetID(types.RandomATXID())
	blob := types.AtxBlob{Blob: []byte("original blob")}
	require.NoError(t, atxs.Add(db, &atx, blob))
	require.Equal(t, 2, db.QueryCount()) // insert atx + blob

	b := &sql.Blob{}
	_, err := atxs.LoadBlob(context.Background(), db, atx.ID().Bytes(), b)
	require.NoError(t, err)
	require.Equal(t, blob.Blob, b.Bytes)

	atx2 := types.ActivationTx{}
	atx2.SetID(types.RandomATXID())
	blob2 := types.AtxBlob{Blob: []byte("other blob")}
	require.Less(t, len(blob2.Blob), len(blob.Blob))
	require.NoError(t, atxs.Add(db, &atx2, blob2))

	// Loading atx2 doesn't overwrite the cached blob for atx1
	_, err = atxs.LoadBlob(context.Background(), db, atx2.ID().Bytes(), b)
	require.NoError(t, err)
	require.Equal(t, blob2.Blob, b.Bytes)

	_, err = atxs.LoadBlob(context.Background(), db, atx.ID().Bytes(), b)
	require.NoError(t, err)
	require.Equal(t, blob.Blob, b.Bytes)
}

// Test that the cached blob is not shared with the caller
// but copied into the provided blob.
func TestGetBlobCached_OverwriteSafety(t *testing.T) {
	db := sql.InMemory(sql.WithQueryCache(true))
	atx := types.ActivationTx{}
	atx.SetID(types.RandomATXID())
	blob := types.AtxBlob{Blob: []byte("original blob")}
	require.NoError(t, atxs.Add(db, &atx, blob))
	require.Equal(t, 2, db.QueryCount()) // insert atx + blob

	var b sql.Blob // we will reuse the blob between queries
	_, err := atxs.LoadBlob(context.Background(), db, atx.ID().Bytes(), &b)
	require.NoError(t, err)
	require.Equal(t, blob.Blob, b.Bytes)
	b.Bytes[0] = 'X' // modify the blob
	_, err = atxs.LoadBlob(context.Background(), db, atx.ID().Bytes(), &b)
	require.NoError(t, err)
	require.Equal(t, blob.Blob, b.Bytes)
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
	addedATXs := make([]*types.ActivationTx, 11)
	blobs := make([][]byte, 11)
	var b sql.Blob
	for n := range addedATXs {
		atx, blob := newAtx(t, sig, withPublishEpoch(1))
		require.NoError(t, atxs.Add(db, atx, blob))
		addedATXs[n] = atx
		blobs[n] = blob.Blob
		_, err := atxs.LoadBlob(ctx, db, atx.ID().Bytes(), &b)
		require.NoError(t, err)
		require.Equal(t, blob.Blob, b.Bytes)
	}

	// insert atx + insert blob + load blob each time
	require.Equal(t, 33, db.QueryCount())

	// The ATXs except the first one stay in place
	for n, atx := range addedATXs[1:] {
		_, err := atxs.LoadBlob(ctx, db, atx.ID().Bytes(), &b)
		require.NoError(t, err)
		require.Equal(t, blobs[n+1], b.Bytes)
		require.Equal(t, 33, db.QueryCount())
	}

	// The first ATX is evicted. We check it after the loop to avoid additional evictions.
	_, err = atxs.LoadBlob(ctx, db, addedATXs[0].ID().Bytes(), &b)
	require.NoError(t, err)
	require.Equal(t, blobs[0], b.Bytes)
	require.Equal(t, 34, db.QueryCount())
}

func TestCheckpointATX(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, _ := newAtx(t, sig, withPublishEpoch(3), withSequence(4))
	catx := &atxs.CheckpointAtx{
		ID:             atx.ID(),
		Epoch:          atx.PublishEpoch,
		CommitmentATX:  types.ATXID{1, 2, 3},
		VRFNonce:       types.VRFPostIndex(119),
		NumUnits:       atx.NumUnits,
		BaseTickHeight: 1000,
		TickCount:      atx.TickCount + 1,
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
	require.Equal(t, catx.BaseTickHeight, got.BaseTickHeight)
	require.Equal(t, catx.TickCount, got.TickCount)
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
	_, err = atxs.LoadBlob(ctx, db, catx.ID.Bytes(), &blob)
	require.NoError(t, err)
	require.Empty(t, blob.Bytes)
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	nonExistingATXID := types.ATXID(types.CalcHash32([]byte("0")))
	_, err := atxs.Get(db, nonExistingATXID)
	require.ErrorIs(t, err, sql.ErrNotFound)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, blob := newAtx(t, sig, withPublishEpoch(1))

	require.NoError(t, atxs.Add(db, atx, blob))
	require.ErrorIs(t, atxs.Add(db, atx, blob), sql.ErrObjectExists)

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
		atx.VRFNonce = nonce
	}
}

func withPrevATXID(id types.ATXID) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.PrevATXID = id
	}
}

func withCoinbase(addr types.Address) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.Coinbase = addr
	}
}

func newAtx(t testing.TB, signer *signing.EdSigner, opts ...createAtxOpt) (*types.ActivationTx, types.AtxBlob) {
	nonce := uint64(123)
	watx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PrevATXID: types.RandomATXID(),
			},
			NumUnits: 2,
			VRFNonce: &nonce,
		},
	}
	watx.Sign(signer)

	atx := fixture.ToAtx(t, watx)
	for _, opt := range opts {
		opt(atx)
	}
	return atx, watx.Blob()
}

type header struct {
	coinbase    types.Address
	base, count uint64
	epoch       types.EpochID
	malicious   bool
	filteredOut bool
}

func createAtx(tb testing.TB, db *sql.Database, hdr header) (types.ATXID, *signing.EdSigner) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)

	full := &types.ActivationTx{
		PublishEpoch:   hdr.epoch,
		Coinbase:       hdr.coinbase,
		NumUnits:       2,
		BaseTickHeight: hdr.base,
		TickCount:      hdr.count,
		SmesherID:      sig.NodeID(),
	}
	full.SetReceived(time.Now())
	full.SetID(types.RandomATXID())

	require.NoError(tb, atxs.Add(db, full, types.AtxBlob{}))
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
					PublishEpoch: types.EpochID(epoch),
					NumUnits:     1,
					TickCount:    1,
				}
				full.SetReceived(time.Now())
				full.SetID(types.ATXID{byte(i)})
				require.NoError(t, atxs.Add(db, full, types.AtxBlob{}))
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

	atx1, blob1 := newAtx(t, sig, withPublishEpoch(1), withPrevATXID(prevATXID))
	atx2, blob2 := newAtx(t, sig, withPublishEpoch(2), withPrevATXID(prevATXID))

	require.NoError(t, atxs.Add(db, atx1, blob1))
	require.NoError(t, atxs.Add(db, atx2, blob2))

	// verify that the ATXs were added
	got1, err := atxs.Get(db, atx1.ID())
	require.NoError(t, err)
	require.Equal(t, atx1, got1)

	got2, err := atxs.Get(db, atx2.ID())
	require.NoError(t, err)
	require.Equal(t, atx2, got2)

	// add 10 valid ATXs by 10 other smeshers
	for i := 2; i < 6; i++ {
		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx, blob := newAtx(t, otherSig, withPublishEpoch(types.EpochID(i)))
		require.NoError(t, atxs.Add(db, atx, blob))

		atx2, blob2 := newAtx(t, otherSig,
			withPublishEpoch(types.EpochID(i+1)),
			withPrevATXID(atx.ID()),
		)
		require.NoError(t, atxs.Add(db, atx2, blob2))
	}

	// get the collisions
	got, err := atxs.PrevATXCollisions(db)
	require.NoError(t, err)
	require.Len(t, got, 1)

	require.Equal(t, sig.NodeID(), got[0].NodeID1)
	require.Equal(t, sig.NodeID(), got[0].NodeID2)
	require.ElementsMatch(t, []types.ATXID{atx1.ID(), atx2.ID()}, []types.ATXID{got[0].ATX1, got[0].ATX2})
}

func TestCoinbase(t *testing.T) {
	t.Parallel()
	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		_, err := atxs.Coinbase(db, types.NodeID{})
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
	t.Run("found", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx, blob := newAtx(t, sig, withCoinbase(types.Address{1, 2, 3}))
		require.NoError(t, atxs.Add(db, atx, blob))
		cb, err := atxs.Coinbase(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, atx.Coinbase, cb)
	})
	t.Run("picks last", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx1, blob1 := newAtx(t, sig, withPublishEpoch(1), withCoinbase(types.Address{1, 2, 3}))
		atx2, blob2 := newAtx(t, sig, withPublishEpoch(2), withCoinbase(types.Address{4, 5, 6}))
		require.NoError(t, atxs.Add(db, atx1, blob1))
		require.NoError(t, atxs.Add(db, atx2, blob2))
		cb, err := atxs.Coinbase(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, atx2.Coinbase, cb)
	})
}
