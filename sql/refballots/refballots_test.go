package refballots

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const baseEpoch = 3

func TestGet(t *testing.T) {
	db := sql.InMemory()

	refBallots := []types.BallotID{
		{1},
		{2},
		{3},
	}

	for i, refBallot := range refBallots {
		require.NoError(t, Add(db, types.EpochID(baseEpoch+i), refBallot))
	}

	for i := range refBallots {
		refBallot, err := Get(db, types.EpochID(baseEpoch+i))
		require.NoError(t, err)
		require.Equal(t, refBallots[i], refBallot)
	}

	_, err := Get(db, types.EpochID(0))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	_, err := Get(db, types.EpochID(baseEpoch))
	require.ErrorIs(t, err, sql.ErrNotFound)

	refBallot := types.BallotID{1}
	require.NoError(t, Add(db, baseEpoch, refBallot))
	require.ErrorIs(t, Add(db, baseEpoch, refBallot), sql.ErrObjectExists)

	got, err := Get(db, baseEpoch)
	require.NoError(t, err)
	require.Equal(t, refBallot, got)
}
