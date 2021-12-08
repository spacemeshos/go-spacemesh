package tortoise

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestVerifyingIsGood(t *testing.T) {
	goodbase := types.BallotID{0}
	ballots := []types.BallotID{{1}, {2}, {3}}
	blocks := []types.BlockID{{11}, {22}, {33}}
	for _, tc := range []struct {
		desc string
		commonState
		ballot tortoiseBallot
		expect bool
	}{
		{
			desc: "BadBeaconBallot",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{
					ballots[0]: {},
				},
			},
			ballot: tortoiseBallot{id: ballots[0]},
		},
		{
			desc: "BadBaseBallot",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
			},
			ballot: tortoiseBallot{id: ballots[0], base: ballots[1]},
		},
		{
			desc: "ExceptionBeforeBaseBallot",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					goodbase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{
					blocks[0]: types.NewLayerID(9),
				},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				votes: Opinion{
					blocks[0]: support,
				},
			},
		},
		{
			desc: "ExceptionNotInMemory",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					goodbase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				votes: Opinion{
					blocks[0]: support,
				},
			},
		},
		{
			desc: "DifferentOpinionOnException",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					goodbase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{
					blocks[0]: types.NewLayerID(10),
				},
				localOpinion: Opinion{
					blocks[0]: against,
				},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				votes: Opinion{
					blocks[0]: support,
				},
			},
		},
		{
			desc: "GoodBallot",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					goodbase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{
					blocks[0]: types.NewLayerID(10),
				},
				localOpinion: Opinion{
					blocks[0]: support,
					blocks[1]: against,
					blocks[2]: against,
				},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				votes: Opinion{
					blocks[0]: support,
					blocks[1]: against,
					blocks[2]: against,
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			v := newVerifying(Config{}, &tc.commonState)
			v.goodBallots[goodbase] = struct{}{}
			require.Equal(t, tc.expect, v.isGood(logger, tc.ballot))
		})
	}
}
