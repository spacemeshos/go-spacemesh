package ballots

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func TestLayer(t *testing.T) {
	db := sql.InMemory()
	start := types.NewLayerID(1)
	pub := types.BytesToNodeID([]byte{1, 1, 1})
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, nil, pub, types.BallotMetadata{Layer: start}),
		types.NewExistingBallot(types.BallotID{2}, nil, pub, types.BallotMetadata{Layer: start}),
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

	require.NoError(t, identities.SetMalicious(db, pub, []byte("proof")))
	rst, err = Layer(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for _, ballot := range rst {
		require.True(t, ballot.IsMalicious())
	}
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()
	pub := types.BytesToNodeID([]byte{1, 1})
	ballot := types.NewExistingBallot(types.BallotID{1}, []byte{1, 1}, pub, types.BallotMetadata{})
	_, err := Get(db, ballot.ID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, Add(db, &ballot))
	require.ErrorIs(t, Add(db, &ballot), sql.ErrObjectExists)

	stored, err := Get(db, ballot.ID())
	require.NoError(t, err)
	require.Equal(t, &ballot, stored)

	require.NoError(t, identities.SetMalicious(db, pub, []byte("proof")))
	stored, err = Get(db, ballot.ID())
	require.NoError(t, err)
	require.True(t, stored.IsMalicious())
}

func TestHas(t *testing.T) {
	db := sql.InMemory()
	ballot := types.NewExistingBallot(types.BallotID{1}, []byte{}, types.NodeID{}, types.BallotMetadata{})

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
	require.Equal(t, types.LayerID{}, latest)

	ballot := types.NewExistingBallot(types.BallotID{1}, []byte{}, types.NodeID{}, types.BallotMetadata{Layer: types.NewLayerID(11)})
	require.NoError(t, Add(db, &ballot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, ballot.Layer, latest)

	newBallot := types.NewExistingBallot(types.BallotID{2}, []byte{}, types.NodeID{}, types.BallotMetadata{Layer: types.NewLayerID(12)})
	require.NoError(t, Add(db, &newBallot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, newBallot.Layer, latest)
}

func TestCountByPubkeyLayer(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(1)
	pub1 := types.BytesToNodeID([]byte{1, 1, 1})
	pub2 := types.BytesToNodeID([]byte{2, 2, 2})
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, nil, pub1, types.BallotMetadata{Layer: lid}),
		types.NewExistingBallot(types.BallotID{2}, nil, pub1, types.BallotMetadata{Layer: lid.Add(1)}),
		types.NewExistingBallot(types.BallotID{3}, nil, pub2, types.BallotMetadata{Layer: lid}),
		types.NewExistingBallot(types.BallotID{4}, nil, pub2, types.BallotMetadata{Layer: lid}),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	count, err := CountByPubkeyLayer(db, lid, pub1.Bytes())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = CountByPubkeyLayer(db, lid, pub2.Bytes())
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestLayerBallotBySmesher(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(1)
	pub1 := types.BytesToNodeID([]byte{1, 1, 1})
	pub2 := types.BytesToNodeID([]byte{2, 2, 2})
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{2}, nil, pub1, types.BallotMetadata{Layer: lid.Add(1)}),
		types.NewExistingBallot(types.BallotID{3}, nil, pub2, types.BallotMetadata{Layer: lid}),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	prev, err := LayerBallotByNodeID(db, lid, pub1)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, prev)
	prev, err = LayerBallotByNodeID(db, lid, pub2)
	require.NoError(t, err)
	require.NotNil(t, prev)
	require.Equal(t, ballots[1], *prev)
}

func TestGetRefBallot(t *testing.T) {
	types.SetLayersPerEpoch(3)
	db := sql.InMemory()
	lid2 := types.NewLayerID(2)
	lid3 := types.NewLayerID(3)
	lid4 := types.NewLayerID(4)
	lid5 := types.NewLayerID(5)
	lid6 := types.NewLayerID(6)
	pub1 := types.BytesToNodeID([]byte{1, 1, 1})
	pub2 := types.BytesToNodeID([]byte{2, 2, 2})
	pub3 := types.BytesToNodeID([]byte{3, 3, 3})
	pub4 := types.BytesToNodeID([]byte{4, 4, 4})
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, nil, pub1, types.BallotMetadata{Layer: lid2}),
		types.NewExistingBallot(types.BallotID{2}, nil, pub1, types.BallotMetadata{Layer: lid3}),
		types.NewExistingBallot(types.BallotID{3}, nil, pub2, types.BallotMetadata{Layer: lid3}),
		types.NewExistingBallot(types.BallotID{4}, nil, pub2, types.BallotMetadata{Layer: lid4}),
		types.NewExistingBallot(types.BallotID{5}, nil, pub3, types.BallotMetadata{Layer: lid5}),
		types.NewExistingBallot(types.BallotID{6}, nil, pub4, types.BallotMetadata{Layer: lid6}),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	count, err := GetRefBallot(db, 1, pub1.Bytes())
	require.NoError(t, err)
	require.Equal(t, types.BallotID{2}, count)

	count, err = GetRefBallot(db, 1, pub2.Bytes())
	require.NoError(t, err)
	require.Equal(t, types.BallotID{3}, count)

	count, err = GetRefBallot(db, 1, pub3.Bytes())
	require.NoError(t, err)
	require.Equal(t, types.BallotID{5}, count)

	_, err = GetRefBallot(db, 1, pub4.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}
