package identities

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestMalicious(t *testing.T) {
	db := sql.InMemory()

	nodeID := types.NodeID{1, 1, 1, 1}
	mal, err := IsMalicious(db, nodeID)
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
	now := time.Now()
	data, err := codec.Encode(proof)
	require.NoError(t, err)
	require.NoError(t, SetMalicious(db, nodeID, data, now))

	mal, err = IsMalicious(db, nodeID)
	require.NoError(t, err)
	require.True(t, mal)

	mal, err = IsMalicious(db, types.RandomNodeID())
	require.NoError(t, err)
	require.False(t, mal)

	got, err := GetMalfeasanceProof(db, nodeID)
	require.NoError(t, err)
	require.Equal(t, now.UTC(), got.Received().UTC())
	got.SetReceived(time.Time{})
	require.EqualValues(t, proof, got)
}

func Test_GetMalicious(t *testing.T) {
	db := sql.InMemory()
	got, err := GetMalicious(db)
	require.NoError(t, err)
	require.Nil(t, got)

	const numBad = 11
	bad := make([]types.NodeID, 0, numBad)
	for i := 0; i < numBad; i++ {
		nid := types.NodeID{byte(i + 1)}
		bad = append(bad, nid)
		require.NoError(t, SetMalicious(db, nid, types.RandomBytes(11), time.Now().Local()))
	}
	got, err = GetMalicious(db)
	require.NoError(t, err)
	require.Equal(t, bad, got)
}

func TestLoadMalfeasanceBlob(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	nid1 := types.RandomNodeID()
	proof1 := types.RandomBytes(11)
	SetMalicious(db, nid1, proof1, time.Now().Local())

	var blob1 sql.Blob
	require.NoError(t, LoadMalfeasanceBlob(ctx, db, nid1.Bytes(), &blob1))
	require.Equal(t, proof1, blob1.Bytes)

	blobSizes, err := GetBlobSizes(db, [][]byte{nid1.Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes)}, blobSizes)

	nid2 := types.RandomNodeID()
	proof2 := types.RandomBytes(12)
	SetMalicious(db, nid2, proof2, time.Now().Local())

	var blob2 sql.Blob
	require.NoError(t, LoadMalfeasanceBlob(ctx, db, nid2.Bytes(), &blob2))
	require.Equal(t, proof2, blob2.Bytes)
	blobSizes, err = GetBlobSizes(db, [][]byte{
		nid1.Bytes(),
		nid2.Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes)}, blobSizes)

	noSuchID := types.RandomATXID()
	require.ErrorIs(t, LoadMalfeasanceBlob(ctx, db, noSuchID[:], &sql.Blob{}), sql.ErrNotFound)

	blobSizes, err = GetBlobSizes(db, [][]byte{
		nid1.Bytes(),
		noSuchID.Bytes(),
		nid2.Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), -1, len(blob2.Bytes)}, blobSizes)
}

func TestMarried(t *testing.T) {
	t.Parallel()
	t.Run("identity not in DB", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()

		id := types.RandomNodeID()
		married, err := Married(db, id)
		require.NoError(t, err)
		require.False(t, married)

		atx := types.RandomATXID()
		require.NoError(t, SetMarriage(db, id, atx))

		married, err = Married(db, id)
		require.NoError(t, err)
		require.True(t, married)
	})
	t.Run("identity in DB", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()

		id := types.RandomNodeID()
		// add ID in the DB
		SetMalicious(db, id, types.RandomBytes(11), time.Now())

		married, err := Married(db, id)
		require.NoError(t, err)
		require.False(t, married)

		require.NoError(t, SetMarriage(db, id, types.RandomATXID()))

		married, err = Married(db, id)
		require.NoError(t, err)
		require.True(t, married)
	})
}

func TestEquivocationSet(t *testing.T) {
	t.Parallel()
	t.Run("equivocation set of married IDs", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()

		atx := types.RandomATXID()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		for _, id := range ids {
			require.NoError(t, SetMarriage(db, id, atx))
		}

		for _, id := range ids {
			married, err := Married(db, id)
			require.NoError(t, err)
			require.True(t, married)
			set, err := EquivocationSet(db, id)
			require.NoError(t, err)
			require.ElementsMatch(t, ids, set)
		}
	})
	t.Run("equivocation set for unmarried ID contains itself only", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		id := types.RandomNodeID()
		set, err := EquivocationSet(db, id)
		require.NoError(t, err)
		require.Equal(t, []types.NodeID{id}, set)
	})
	t.Run("can't escape the marriage", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		atx := types.RandomATXID()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		for _, id := range ids {
			require.NoError(t, SetMarriage(db, id, atx))
		}

		for _, id := range ids {
			set, err := EquivocationSet(db, id)
			require.NoError(t, err)
			require.ElementsMatch(t, ids, set)
		}

		// try to marry via another random ATX
		// the set should remain intact
		require.NoError(t, SetMarriage(db, ids[0], types.RandomATXID()))
		for _, id := range ids {
			set, err := EquivocationSet(db, id)
			require.NoError(t, err)
			require.ElementsMatch(t, ids, set)
		}
	})
	t.Run("married doesn't become malicious immediately", func(t *testing.T) {
		db := sql.InMemory()
		atx := types.RandomATXID()
		id := types.RandomNodeID()
		require.NoError(t, SetMarriage(db, id, atx))

		malicious, err := IsMalicious(db, id)
		require.NoError(t, err)
		require.False(t, malicious)

		proof, err := GetMalfeasanceProof(db, id)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, proof)

		ids, err := GetMalicious(db)
		require.NoError(t, err)
		require.Empty(t, ids)
	})
	t.Run("all IDs in equivocation set are malicious if one is", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		atx := types.RandomATXID()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		for _, id := range ids {
			require.NoError(t, SetMarriage(db, id, atx))
		}

		require.NoError(t, SetMalicious(db, ids[0], []byte("proof"), time.Now()))

		for _, id := range ids {
			malicious, err := IsMalicious(db, id)
			require.NoError(t, err)
			require.True(t, malicious)
		}
	})
}
