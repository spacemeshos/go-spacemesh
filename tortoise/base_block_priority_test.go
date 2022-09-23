package tortoise

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPrioritizeBlocks(t *testing.T) {
	ballots := []*ballotInfo{
		{id: types.BallotID{1}},
		{id: types.BallotID{2}},
		{id: types.BallotID{3}},
		{id: types.BallotID{4}},
	}
	for _, tc := range []struct {
		desc             string
		input            []*ballotInfo
		disagrements     map[types.BallotID]types.LayerID
		badBeaconBallots map[types.BallotID]struct{}
		expect           []types.BallotID
	}{
		{
			desc:   "SortLexically",
			input:  ballots,
			expect: []types.BallotID{ballots[0].id, ballots[1].id, ballots[2].id, ballots[3].id},
		},
		{
			desc:   "PrioritizeWithHigherDisagreementLayer",
			input:  ballots,
			expect: []types.BallotID{ballots[3].id, ballots[2].id, ballots[0].id, ballots[1].id},
			disagrements: map[types.BallotID]types.LayerID{
				ballots[2].id: types.NewLayerID(9),
				ballots[3].id: types.NewLayerID(10),
			},
		},
		{
			desc:   "PrioritizeByHigherLayer",
			expect: []types.BallotID{ballots[2].id, ballots[3].id, ballots[1].id, ballots[0].id},
			disagrements: map[types.BallotID]types.LayerID{
				ballots[2].id: types.NewLayerID(9),
				ballots[3].id: types.NewLayerID(9),
			},
			input: []*ballotInfo{
				{id: ballots[0].id},
				{id: ballots[1].id, layer: types.NewLayerID(9)},
				{id: ballots[2].id, layer: types.NewLayerID(10)},
				{id: ballots[3].id},
			},
		},
		{
			desc:   "PrioritizeBallotsWithGoodBeacon",
			input:  ballots,
			expect: []types.BallotID{ballots[1].id, ballots[2].id, ballots[3].id, ballots[0].id},
			disagrements: map[types.BallotID]types.LayerID{
				ballots[0].id: types.NewLayerID(8),
				ballots[1].id: types.NewLayerID(9),
				ballots[2].id: types.NewLayerID(9),
				ballots[3].id: types.NewLayerID(9),
			},
			badBeaconBallots: map[types.BallotID]struct{}{
				ballots[0].id: {},
				ballots[3].id: {},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			rst := make([]*ballotInfo, len(tc.input))
			copy(rst, tc.input)

			rng := rand.New(rand.NewSource(10001))
			rng.Shuffle(len(rst), func(i, j int) {
				rst[i], rst[j] = rst[j], rst[i]
			})

			prioritizeBallots(rst, tc.disagrements, tc.badBeaconBallots)
			for i := range tc.expect {
				require.Equal(t, tc.expect[i], rst[i].id)
			}
		})
	}
}
