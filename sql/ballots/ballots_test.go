package ballots

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const layersPerEpoch = 3

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func TestLayer(t *testing.T) {
	db := sql.InMemory()
	start := types.LayerID(1)
	pub := types.BytesToNodeID([]byte{1, 1, 1})
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, pub, start),
		types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, pub, start),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	ids, err := IDsInLayer(db, start)
	require.NoError(t, err)
	require.Len(t, ids, len(ballots))

	rst, err := Layer(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for i, ballot := range rst {
		require.Equal(t, &ballots[i], ballot)
	}

	require.NoError(t, identities.SetMalicious(db, pub, []byte("proof"), time.Now()))
	rst, err = Layer(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for _, ballot := range rst {
		require.True(t, ballot.IsMalicious())
	}

	rst, err = LayerNoMalicious(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for _, ballot := range rst {
		require.False(t, ballot.IsMalicious())
	}
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))
	_, err := Get(db, ballot.ID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, Add(db, &ballot))
	require.ErrorIs(t, Add(db, &ballot), sql.ErrObjectExists)

	stored, err := Get(db, ballot.ID())
	require.NoError(t, err)
	require.Equal(t, &ballot, stored)

	require.NoError(t, identities.SetMalicious(db, nodeID, []byte("proof"), time.Now()))
	stored, err = Get(db, ballot.ID())
	require.NoError(t, err)
	require.True(t, stored.IsMalicious())
}

func TestHas(t *testing.T) {
	db := sql.InMemory()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, types.EmptyNodeID, types.LayerID(0))

	exists, err := Has(db, ballot.ID())
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, Add(db, &ballot))
	exists, err = Has(db, ballot.ID())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestLatest(t *testing.T) {
	db := sql.InMemory()
	latest, err := LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, types.LayerID(0), latest)

	ballot := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, types.EmptyNodeID, types.LayerID(11))
	require.NoError(t, Add(db, &ballot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, ballot.Layer, latest)

	newBallot := types.NewExistingBallot(
		types.BallotID{2},
		types.EmptyEdSignature,
		types.EmptyNodeID,
		types.LayerID(12),
	)
	require.NoError(t, Add(db, &newBallot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, newBallot.Layer, latest)
}

func TestLayerBallotBySmesher(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(1)
	nodeID1 := types.RandomNodeID()
	nodeID2 := types.RandomNodeID()
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, nodeID1, lid.Add(1)),
		types.NewExistingBallot(types.BallotID{3}, types.EmptyEdSignature, nodeID2, lid),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	prev, err := LayerBallotByNodeID(db, lid, nodeID1)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, prev)
	prev, err = LayerBallotByNodeID(db, lid, nodeID2)
	require.NoError(t, err)
	require.NotNil(t, prev)
	require.Equal(t, ballots[1], *prev)
}

func newAtx(signer *signing.EdSigner, layerID types.LayerID) *types.ActivationTx {
	atx := &types.ActivationTx{
		PublishEpoch: layerID.GetEpoch(),
		NumUnits:     2,
		TickCount:    1,
		SmesherID:    signer.NodeID(),
	}
	atx.SetID(types.ATXID{1, 2, 3})
	atx.SetReceived(time.Now().Local())
	return atx
}

func TestFirstInEpoch(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(layersPerEpoch * 2)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx := newAtx(sig, lid)
	require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))

	got, err := FirstInEpoch(db, atx.ID(), 2)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)

	b1 := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, sig.NodeID(), lid)
	b1.AtxID = atx.ID()
	require.NoError(t, Add(db, &b1))
	b2 := types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, sig.NodeID(), lid)
	b2.AtxID = atx.ID()
	b2.EpochData = &types.EpochData{}
	require.NoError(t, Add(db, &b2))
	b3 := types.NewExistingBallot(types.BallotID{3}, types.EmptyEdSignature, sig.NodeID(), lid.Add(1))
	b3.AtxID = atx.ID()
	require.NoError(t, Add(db, &b3))

	got, err = FirstInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.False(t, got.IsMalicious())
	require.Equal(t, got.AtxID, atx.ID())
	require.Equal(t, got.ID(), b1.ID())

	last, err := LastInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.Equal(t, b3.ID(), last.ID())

	require.NoError(t, identities.SetMalicious(db, sig.NodeID(), []byte("bad"), time.Now()))
	got, err = FirstInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.True(t, got.IsMalicious())
	require.Equal(t, got.AtxID, atx.ID())
	require.Equal(t, got.ID(), b1.ID())
}

func TestAllFirstInEpoch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc    string
		target  types.EpochID
		ballots []types.Ballot
		expect  []int // references to ballots field
	}{
		{
			"sanity",
			0,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer()+2,
				),
				types.NewExistingBallot(
					types.BallotID{2}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
			},
			[]int{1},
		},
		{
			"multiple smeshers",
			0,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer()+2,
				),
				types.NewExistingBallot(
					types.BallotID{2}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(1).FirstLayer()-1,
				),
				types.NewExistingBallot(
					types.BallotID{4}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(0).FirstLayer(),
				),
			},
			[]int{1, 3},
		},
		{
			"empty",
			1,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(0).FirstLayer(),
				),
			},
			[]int{},
		},
		{
			"multi epoch",
			1,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{4}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(1).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{5}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(1).FirstLayer(),
				),
			},
			[]int{2, 3},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			db := sql.InMemory()
			for _, ballot := range tc.ballots {
				require.NoError(t, Add(db, &ballot))
			}
			var expect []*types.Ballot
			for _, bi := range tc.expect {
				expect = append(expect, &tc.ballots[bi])
			}
			results, err := AllFirstInEpoch(db, tc.target)
			require.NoError(t, err)
			require.Equal(t, expect, results)
		})
	}
}

func TestLoadBlob(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	ballot1 := types.NewExistingBallot(
		types.RandomBallotID(), types.RandomEdSignature(), types.RandomNodeID(), types.LayerID(0))
	encoded1 := codec.MustEncode(&ballot1)
	require.NoError(t, Add(db, &ballot1))

	ballot2 := types.NewExistingBallot(
		types.RandomBallotID(), types.RandomEdSignature(), types.RandomNodeID(), types.LayerID(0))
	encoded2 := codec.MustEncode(&ballot2)
	require.NoError(t, Add(db, &ballot2))

	var blob1 sql.Blob
	require.NoError(t, LoadBlob(ctx, db, ballot1.ID().Bytes(), &blob1))
	require.Equal(t, encoded1, blob1.Bytes)

	var blob2 sql.Blob
	require.NoError(t, LoadBlob(ctx, db, ballot2.ID().Bytes(), &blob2))
	require.Equal(t, encoded2, blob2.Bytes)

	noSuchID := types.RandomBallotID()
	require.ErrorIs(t, LoadBlob(ctx, db, noSuchID.Bytes(), &sql.Blob{}), sql.ErrNotFound)

	sizes, err := GetBlobSizes(db, [][]byte{
		ballot1.ID().Bytes(),
		ballot2.ID().Bytes(),
		noSuchID.Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes), -1}, sizes)
}
