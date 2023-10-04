package ballots

import (
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

func TestUpdateBlob(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))
	ballot.EpochData = &types.EpochData{
		ActiveSetHash: types.RandomHash(),
	}
	ballot.ActiveSet = types.RandomActiveSet(199)
	require.NoError(t, Add(db, &ballot))
	got, err := Get(db, types.BallotID{1})
	require.NoError(t, err)
	require.Equal(t, ballot, *got)

	ballot.ActiveSet = nil
	require.NoError(t, UpdateBlob(db, types.BallotID{1}, codec.MustEncode(&ballot)))
	got, err = Get(db, types.BallotID{1})
	require.NoError(t, err)
	require.Empty(t, got.ActiveSet)
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

	newBallot := types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, types.EmptyNodeID, types.LayerID(12))
	require.NoError(t, Add(db, &newBallot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, newBallot.Layer, latest)
}

func TestCountByPubkeyLayer(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(1)
	nodeID1 := types.RandomNodeID()
	nodeID2 := types.RandomNodeID()
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, nodeID1, lid),
		types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, nodeID1, lid.Add(1)),
		types.NewExistingBallot(types.BallotID{3}, types.EmptyEdSignature, nodeID2, lid),
		types.NewExistingBallot(types.BallotID{4}, types.EmptyEdSignature, nodeID2, lid),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	count, err := CountByPubkeyLayer(db, lid, nodeID1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = CountByPubkeyLayer(db, lid, nodeID2)
	require.NoError(t, err)
	require.Equal(t, 2, count)
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

func TestRefBallot(t *testing.T) {
	db := sql.InMemory()
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1}, types.EpochID(1).FirstLayer()-1),
		types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, types.NodeID{1}, types.EpochID(1).FirstLayer()),
		types.NewExistingBallot(types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2}, types.EpochID(1).FirstLayer()),
		types.NewExistingBallot(types.BallotID{4}, types.EmptyEdSignature, types.NodeID{2}, types.EpochID(1).FirstLayer()+1),
		types.NewExistingBallot(types.BallotID{5}, types.EmptyEdSignature, types.NodeID{3}, types.EpochID(1).FirstLayer()+2),
		types.NewExistingBallot(types.BallotID{6}, types.EmptyEdSignature, types.NodeID{4}, types.EpochID(2).FirstLayer()),
	}
	for _, i := range []int{0, 1, 2, 4, 5} {
		ballots[i].EpochData = &types.EpochData{Beacon: types.Beacon{1, 2}}
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	got, err := RefBallot(db, 1, types.NodeID{1})
	require.NoError(t, err)
	require.Equal(t, ballots[1], *got)

	got, err = RefBallot(db, 1, types.NodeID{2})
	require.NoError(t, err)
	require.Equal(t, ballots[2], *got)

	got, err = RefBallot(db, 1, types.NodeID{3})
	require.NoError(t, err)
	require.Equal(t, ballots[4], *got)

	_, err = RefBallot(db, 1, types.NodeID{4})
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func newAtx(signer *signing.EdSigner, layerID types.LayerID) (*types.VerifiedActivationTx, error) {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: layerID.GetEpoch(),
				PrevATXID:    types.RandomATXID(),
			},
			NumUnits: 2,
		},
	}

	nodeID := signer.NodeID()
	atx.SetID(types.ATXID{1, 2, 3})
	atx.SmesherID = nodeID
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx.Verify(0, 1)
}

func TestFirstInEpoch(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(layersPerEpoch * 2)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, lid)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx))

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
	require.Equal(t, got.ID(), b2.ID())

	require.NoError(t, identities.SetMalicious(db, sig.NodeID(), []byte("bad"), time.Now()))
	got, err = FirstInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.True(t, got.IsMalicious())
	require.Equal(t, got.AtxID, atx.ID())
	require.Equal(t, got.ID(), b2.ID())
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
		tc := tc
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
