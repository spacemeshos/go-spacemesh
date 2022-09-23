package tortoise

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPrioritizeBlocks(t *testing.T) {
	ballots := []*ballotInfoV2{
		{id: types.BallotID{1}},
		{id: types.BallotID{2}},
		{id: types.BallotID{3}},
		{id: types.BallotID{4}},
	}
	for _, tc := range []struct {
		desc             string
		input            []*ballotInfoV2
		disagrements     map[types.BallotID]types.LayerID
		badBeaconBallots map[types.BallotID]struct{}
		expect           []*ballotInfoV2
	}{
		{
			desc:   "SortLexically",
			input:  ballots,
			expect: ballots,
		},
		{
			desc:   "PrioritizeWithHigherDisagreementLayer",
			input:  ballots,
			expect: append([]*ballotInfoV2{ballots[3], ballots[2]}, ballots[:2]...),
			disagrements: map[types.BallotID]types.LayerID{
				ballots[2].id: types.NewLayerID(9),
				ballots[3].id: types.NewLayerID(10),
			},
		},
		{
			desc:   "PrioritizeByHigherLayer",
			expect: append([]*ballotInfoV2{ballots[2], ballots[1]}, ballots[0], ballots[3]),
			disagrements: map[types.BallotID]types.LayerID{
				ballots[2].id: types.NewLayerID(9),
				ballots[3].id: types.NewLayerID(9),
			},
			input: []*ballotInfoV2{
				{id: types.BallotID{1}},
				{id: types.BallotID{2}, layer: types.NewLayerID(9)},
				{id: types.BallotID{3}, layer: types.NewLayerID(10)},
				{id: types.BallotID{4}},
			},
		},
		{
			desc:   "PrioritizeBallotsWithGoodBeacon",
			input:  ballots,
			expect: []*ballotInfoV2{ballots[1], ballots[2], ballots[3], ballots[0]},
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
			rst := make([]*ballotInfoV2, len(tc.input))
			copy(rst, tc.input)

			rng := rand.New(rand.NewSource(10001))
			rng.Shuffle(len(rst), func(i, j int) {
				rst[i], rst[j] = rst[j], rst[i]
			})

			prioritizeBallots(rst, tc.disagrements, tc.badBeaconBallots)
			require.Equal(t, tc.expect, rst)
		})
	}
}
