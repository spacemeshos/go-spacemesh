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

func TestMarriageATX(t *testing.T) {
	t.Parallel()
	t.Run("not married", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemory()

		id := types.RandomNodeID()
		_, err := identities.MarriageATX(db, id)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
	t.Run("married", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemory()

		id := types.RandomNodeID()
		marriage := identities.MarriageData{
			ATX:       types.RandomATXID(),
			Signature: types.RandomEdSignature(),
			Index:     2,
			Target:    types.RandomNodeID(),
		}
		require.NoError(t, identities.SetMarriage(db, id, &marriage))
		got, err := identities.MarriageATX(db, id)
		require.NoError(t, err)
		require.Equal(t, marriage.ATX, got)
	})
}

func TestMarriage(t *testing.T) {
	t.Parallel()

	db := statesql.InMemory()

	id := types.RandomNodeID()
	marriage := identities.MarriageData{
		ATX:       types.RandomATXID(),
		Signature: types.RandomEdSignature(),
		Index:     2,
		Target:    types.RandomNodeID(),
	}
	require.NoError(t, identities.SetMarriage(db, id, &marriage))
	got, err := identities.Marriage(db, id)
	require.NoError(t, err)
	require.Equal(t, marriage, *got)
}

func TestEquivocationSet(t *testing.T) {
	t.Parallel()
	t.Run("equivocation set of married IDs", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemory()

		atx := types.RandomATXID()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		for i, id := range ids {
			err := identities.SetMarriage(db, id, &identities.MarriageData{
				ATX:   atx,
				Index: i,
			})
			require.NoError(t, err)
		}

		for _, id := range ids {
			mAtx, err := identities.MarriageATX(db, id)
			require.NoError(t, err)
			require.Equal(t, atx, mAtx)
			set, err := identities.EquivocationSet(db, id)
			require.NoError(t, err)
			require.ElementsMatch(t, ids, set)
		}
	})
	t.Run("equivocation set for unmarried ID contains itself only", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemory()
		id := types.RandomNodeID()
		set, err := identities.EquivocationSet(db, id)
		require.NoError(t, err)
		require.Equal(t, []types.NodeID{id}, set)
	})
	t.Run("can't escape the marriage", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemory()
		atx := types.RandomATXID()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		for i, id := range ids {
			err := identities.SetMarriage(db, id, &identities.MarriageData{
				ATX:   atx,
				Index: i,
			})
			require.NoError(t, err)
		}

		for _, id := range ids {
			set, err := identities.EquivocationSet(db, id)
			require.NoError(t, err)
			require.ElementsMatch(t, ids, set)
		}

		// try to marry via another random ATX
		// the set should remain intact
		err := identities.SetMarriage(db, ids[0], &identities.MarriageData{
			ATX: types.RandomATXID(),
		})
		require.NoError(t, err)
		for _, id := range ids {
			set, err := identities.EquivocationSet(db, id)
			require.NoError(t, err)
			require.ElementsMatch(t, ids, set)
		}
	})
	t.Run("married doesn't become malicious immediately", func(t *testing.T) {
		db := statesql.InMemory()
		atx := types.RandomATXID()
		id := types.RandomNodeID()
		require.NoError(t, identities.SetMarriage(db, id, &identities.MarriageData{ATX: atx}))

		malicious, err := identities.IsMalicious(db, id)
		require.NoError(t, err)
		require.False(t, malicious)

		var blob sql.Blob
		err = identities.LoadMalfeasanceBlob(context.Background(), db, id.Bytes(), &blob)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, blob.Bytes)

		ids, err := identities.GetMalicious(db)
		require.NoError(t, err)
		require.Empty(t, ids)
	})
	t.Run("all IDs in equivocation set are malicious if one is", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemory()
		atx := types.RandomATXID()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		for i, id := range ids {
			require.NoError(t, identities.SetMarriage(db, id, &identities.MarriageData{ATX: atx, Index: i}))
		}

		require.NoError(t, identities.SetMalicious(db, ids[0], []byte("proof"), time.Now()))

		for _, id := range ids {
			malicious, err := identities.IsMalicious(db, id)
			require.NoError(t, err)
			require.True(t, malicious)
		}
	})
}

func TestEquivocationSetByMarriageATX(t *testing.T) {
	t.Parallel()

	t.Run("married IDs", func(t *testing.T) {
		db := statesql.InMemory()
		ids := []types.NodeID{
			types.RandomNodeID(),
			types.RandomNodeID(),
			types.RandomNodeID(),
			types.RandomNodeID(),
		}
		atx := types.RandomATXID()
		for i, id := range ids {
			require.NoError(t, identities.SetMarriage(db, id, &identities.MarriageData{ATX: atx, Index: i}))
		}
		set, err := identities.EquivocationSetByMarriageATX(db, atx)
		require.NoError(t, err)
		require.Equal(t, ids, set)
	})
	t.Run("empty set", func(t *testing.T) {
		db := statesql.InMemory()
		set, err := identities.EquivocationSetByMarriageATX(db, types.RandomATXID())
		require.NoError(t, err)
		require.Empty(t, set)
	})
}
