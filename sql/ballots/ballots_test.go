package ballots

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestLayer(t *testing.T) {
	db := sql.InMemory()
	start := types.NewLayerID(1)
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, nil, nil, types.InnerBallot{LayerIndex: start}),
		types.NewExistingBallot(types.BallotID{2}, nil, nil, types.InnerBallot{LayerIndex: start}),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	ids, err := LayerIDs(db, start)
	require.NoError(t, err)
	require.Len(t, ids, len(ballots))

	rst, err := Layer(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for i, ballot := range rst {
		require.Equal(t, &ballots[i], ballot)
	}
}
