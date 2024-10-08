package identities_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestMalicious(t *testing.T) {
	db := statesql.InMemory()

	nodeID := types.NodeID{1, 1, 1, 1}
	mal, err := identities.IsMalicious(db, nodeID)
	require.NoError(t, err)
	require.False(t, mal)

	var ballotProof wire.BallotProof
	for i := 0; i < 2; i++ {
		ballotProof.Messages[i] = wire.BallotProofMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   types.LayerID(9),
				MsgHash: types.RandomHash(),
			},
			Signature: types.RandomEdSignature(),
			SmesherID: nodeID,
		}
	}
	proof := &wire.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: wire.Proof{
			Type: wire.MultipleBallots,
			Data: &ballotProof,
		},
	}
	require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))

	mal, err = identities.IsMalicious(db, nodeID)
	require.NoError(t, err)
	require.True(t, mal)

	mal, err = identities.IsMalicious(db, types.RandomNodeID())
	require.NoError(t, err)
	require.False(t, mal)

	var blob sql.Blob
	require.NoError(t, identities.LoadMalfeasanceBlob(context.Background(), db, nodeID.Bytes(), &blob))
	got := &wire.MalfeasanceProof{}
	codec.MustDecode(blob.Bytes, got)
	require.Equal(t, proof, got)
}

func Test_GetMalicious(t *testing.T) {
	db := statesql.InMemory()
	got, err := identities.GetMalicious(db)
	require.NoError(t, err)
	require.Nil(t, got)

	const numBad = 11
	bad := make([]types.NodeID, 0, numBad)
	for i := 0; i < numBad; i++ {
		nid := types.NodeID{byte(i + 1)}
		bad = append(bad, nid)
		require.NoError(t, identities.SetMalicious(db, nid, types.RandomBytes(11), time.Now().Local()))
	}
	got, err = identities.GetMalicious(db)
	require.NoError(t, err)
	require.Equal(t, bad, got)
}

func TestLoadMalfeasanceBlob(t *testing.T) {
	db := statesql.InMemory()
	ctx := context.Background()

	nid1 := types.RandomNodeID()
	proof1 := types.RandomBytes(11)
	identities.SetMalicious(db, nid1, proof1, time.Now().Local())

	var blob1 sql.Blob
	require.NoError(t, identities.LoadMalfeasanceBlob(ctx, db, nid1.Bytes(), &blob1))
	require.Equal(t, proof1, blob1.Bytes)

	blobSizes, err := identities.GetBlobSizes(db, [][]byte{nid1.Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes)}, blobSizes)

	nid2 := types.RandomNodeID()
	proof2 := types.RandomBytes(12)
	identities.SetMalicious(db, nid2, proof2, time.Now().Local())

	var blob2 sql.Blob
	require.NoError(t, identities.LoadMalfeasanceBlob(ctx, db, nid2.Bytes(), &blob2))
	require.Equal(t, proof2, blob2.Bytes)
	blobSizes, err = identities.GetBlobSizes(db, [][]byte{
		nid1.Bytes(),
		nid2.Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes)}, blobSizes)

	noSuchID := types.RandomATXID()
	require.ErrorIs(t, identities.LoadMalfeasanceBlob(ctx, db, noSuchID[:], &sql.Blob{}), sql.ErrNotFound)

	blobSizes, err = identities.GetBlobSizes(db, [][]byte{
		nid1.Bytes(),
		noSuchID.Bytes(),
		nid2.Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), -1, len(blob2.Bytes)}, blobSizes)
}

func Test_IterateMaliciousOps(t *testing.T) {
	db := statesql.InMemory()
	tt := []struct {
		id    types.NodeID
		proof []byte
	}{
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
		},
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
		},
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
		},
	}

	for _, tc := range tt {
		err := identities.SetMalicious(db, tc.id, tc.proof, time.Now())
		require.NoError(t, err)
	}

	var got []struct {
		id    types.NodeID
		proof []byte
	}
	err := identities.IterateMaliciousOps(db, builder.Operations{},
		func(id types.NodeID, proof []byte, _ time.Time) bool {
			got = append(got, struct {
				id    types.NodeID
				proof []byte
			}{id, proof})
			return true
		})
	require.NoError(t, err)
	require.ElementsMatch(t, tt, got)
}

func Test_IterateMaliciousOpsWithFilter(t *testing.T) {
	db := statesql.InMemory()
	tt := []struct {
		id    types.NodeID
		proof []byte
	}{
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
		},
		{
			types.RandomNodeID(),
			nil,
		},
		{
			types.RandomNodeID(),
			types.RandomBytes(11),
		},
	}

	for _, tc := range tt {
		err := identities.SetMalicious(db, tc.id, tc.proof, time.Now())
		require.NoError(t, err)
	}

	var got []struct {
		id    types.NodeID
		proof []byte
	}
	ops := builder.Operations{}
	ops.Filter = append(ops.Filter, builder.Op{
		Field: builder.Smesher,
		Token: builder.In,
		Value: [][]byte{tt[0].id.Bytes(), tt[1].id.Bytes()}, // first two ids
	})
	ops.Filter = append(ops.Filter, builder.Op{
		Field: builder.Proof,
		Token: builder.IsNotNull, // only entries which have a proof
	})

	err := identities.IterateMaliciousOps(db, ops, func(id types.NodeID, proof []byte, _ time.Time) bool {
		got = append(got, struct {
			id    types.NodeID
			proof []byte
		}{id, proof})
		return true
	})
	require.NoError(t, err)
	// only the first element should be in the result
	require.ElementsMatch(t, tt[:1], got)
}
