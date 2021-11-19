package tortoise

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPrioritizeBlocks(t *testing.T) {
	ballots := []types.BallotID{
		{1},
		{2},
		{3},
		{4},
	}
	for _, tc := range []struct {
		desc         string
		goodBallots  map[types.BallotID]bool
		disagrements map[types.BallotID]types.LayerID
		ballotLayer  map[types.BallotID]types.LayerID
		expect       []types.BallotID
	}{
		{
			desc:   "SortLexically",
			expect: ballots,
		},
		{
			desc:        "PrioritizeGoodBlocks",
			expect:      append([]types.BallotID{ballots[3]}, ballots[:3]...),
			goodBallots: map[types.BallotID]bool{ballots[3]: false},
		},
		{
			desc:   "PrioritizeWithHigherDisagreementLayer",
			expect: append([]types.BallotID{ballots[3], ballots[2]}, ballots[:2]...),
			goodBallots: map[types.BallotID]bool{
				ballots[2]: false,
				ballots[3]: false,
			},
			disagrements: map[types.BallotID]types.LayerID{
				ballots[2]: types.NewLayerID(9),
				ballots[3]: types.NewLayerID(10),
			},
		},
		{
			desc:   "PrioritizeByHigherLayer",
			expect: append([]types.BallotID{ballots[3], ballots[2]}, ballots[:2]...),
			goodBallots: map[types.BallotID]bool{
				ballots[2]: false,
				ballots[3]: false,
			},
			disagrements: map[types.BallotID]types.LayerID{
				ballots[2]: types.NewLayerID(9),
				ballots[3]: types.NewLayerID(9),
			},
			ballotLayer: map[types.BallotID]types.LayerID{
				ballots[2]: types.NewLayerID(9),
				ballots[3]: types.NewLayerID(10),
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			rst := make([]types.BallotID, len(ballots))
			copy(rst, ballots)

			rng := rand.New(rand.NewSource(10001))
			rng.Shuffle(len(rst), func(i, j int) {
				rst[i], rst[j] = rst[j], rst[i]
			})

			prioritizeBallots(rst, tc.goodBallots, tc.disagrements, tc.ballotLayer)
			require.Equal(t, tc.expect, rst)
		})
	}
}
