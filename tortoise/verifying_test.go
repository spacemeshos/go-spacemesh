package tortoise

import (
	"math/big"
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
				votes: votes{
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
				votes: votes{
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
				localVotes: votes{
					blocks[0]: against,
				},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				votes: votes{
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
				localVotes: votes{
					blocks[0]: support,
					blocks[1]: against,
					blocks[2]: against,
				},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				votes: votes{
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

func TestVerifyingProcessLayer(t *testing.T) {
	var (
		goodbase, badbase = types.BallotID{0}, types.BallotID{0, 1}
		ballots           = []types.BallotID{{1}, {2}, {3}, {4}}
		ballotWeight      = weightFromUint64(10)
		blocks            = []types.BlockID{{11}, {22}, {33}}
		start             = types.NewLayerID(1)
	)
	genCommonState := func() commonState {
		state := newCommonState()
		state.localVotes = votes{
			blocks[0]: support,
			blocks[1]: support,
			blocks[2]: against,
		}
		state.blockLayer = map[types.BlockID]types.LayerID{
			blocks[0]: start.Add(1),
			blocks[1]: start.Add(1),
			blocks[1]: start.Add(2),
		}
		state.ballotLayer = map[types.BallotID]types.LayerID{
			ballots[0]: start.Add(1),
			ballots[1]: start.Add(1),
			ballots[2]: start.Add(2),
			ballots[3]: start.Add(2),
		}
		state.ballotWeight = map[types.BallotID]weight{
			ballots[0]: ballotWeight,
			ballots[1]: ballotWeight,
			ballots[2]: ballotWeight,
			ballots[3]: ballotWeight,
		}
		return state
	}

	for _, tc := range []struct {
		desc         string
		ballots      [][]tortoiseBallot
		layerWeights []weight
		total        []weight
	}{
		{
			desc: "AllGood",
			ballots: [][]tortoiseBallot{
				{
					{
						id: ballots[0], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
					{
						id: ballots[1], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
				},
				{
					{
						id: ballots[2], base: ballots[0], weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
				},
			},
			layerWeights: []weight{weightFromUint64(20), weightFromUint64(10)},
			total:        []weight{weightFromUint64(20), weightFromUint64(30)},
		},
		{
			desc: "AllBad",
			ballots: [][]tortoiseBallot{
				{
					{
						id: ballots[0], base: badbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
					{
						id: ballots[1], base: badbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
				},
				{
					{
						id: ballots[2], base: badbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
				},
			},
			layerWeights: []weight{weightFromUint64(0), weightFromUint64(0)},
			total:        []weight{weightFromUint64(0), weightFromUint64(0)},
		},
		{
			desc: "GoodInFirstLayer",
			ballots: [][]tortoiseBallot{
				{
					{
						id: ballots[0], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
					{
						id: ballots[1], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
				},
				{
					{
						id: ballots[2], base: badbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
					},
				},
			},
			layerWeights: []weight{weightFromUint64(20), weightFromUint64(0)},
			total:        []weight{weightFromUint64(20), weightFromUint64(20)},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			state := genCommonState()
			v := newVerifying(Config{}, &state)
			v.goodBallots[goodbase] = struct{}{}

			for i := range tc.ballots {
				lid := start.Add(uint32(i + 1))
				v.processLayer(logger, lid, tc.ballots[i])
				require.Equal(t, tc.layerWeights[i], v.layerWeights[lid])
				require.Equal(t, tc.total[i], v.totalWeight)
			}
		})
	}
}

func TestVerifyingVerifyLayers(t *testing.T) {
	require.Equal(t, 4, int(types.GetLayersPerEpoch()))
	start := types.NewLayerID(0)
	for _, tc := range []struct {
		desc                string
		epochWeight         map[types.EpochID]weight
		layersWeight        map[types.LayerID]weight
		totalWeight         weight
		verified, processed types.LayerID
		blocks              map[types.LayerID][]types.BlockID
		localOpinion        votes
		config              Config

		expected            types.LayerID
		expectedTotalWeight weight
	}{
		{
			desc: "All",
			epochWeight: map[types.EpochID]weight{
				0: weightFromUint64(10),
				1: weightFromUint64(10),
			},
			layersWeight: map[types.LayerID]weight{
				start.Add(1): weightFromUint64(8),
				start.Add(2): weightFromUint64(8),
				start.Add(3): weightFromUint64(6),
				start.Add(4): weightFromUint64(10),
			},
			totalWeight:  weightFromUint64(32),
			verified:     start,
			processed:    start.Add(4),
			blocks:       map[types.LayerID][]types.BlockID{},
			localOpinion: votes{},
			config: Config{
				LocalThreshold:  big.NewRat(1, 10),
				GlobalThreshold: big.NewRat(7, 10),
			},
			expected:            start.Add(3),
			expectedTotalWeight: weightFromUint64(10),
		},
		{
			desc: "Some",
			epochWeight: map[types.EpochID]weight{
				0: weightFromUint64(10),
				1: weightFromUint64(10),
			},
			layersWeight: map[types.LayerID]weight{
				start.Add(1): weightFromUint64(12),
				start.Add(2): weightFromUint64(14),
				start.Add(3): weightFromUint64(2),
				start.Add(4): weightFromUint64(8),
			},
			totalWeight: weightFromUint64(36),
			verified:    start,
			processed:   start.Add(4),
			config: Config{
				LocalThreshold:  big.NewRat(1, 10),
				GlobalThreshold: big.NewRat(7, 10),
			},
			expected:            start.Add(1),
			expectedTotalWeight: weightFromUint64(24),
		},
		{
			desc: "Undecided",
			epochWeight: map[types.EpochID]weight{
				0: weightFromUint64(10),
				1: weightFromUint64(10),
			},
			layersWeight: map[types.LayerID]weight{
				start.Add(1): weightFromUint64(10),
				start.Add(2): weightFromUint64(10),
				start.Add(3): weightFromUint64(10),
				start.Add(4): weightFromUint64(10),
			},
			totalWeight: weightFromUint64(40),
			verified:    start,
			processed:   start.Add(4),
			blocks: map[types.LayerID][]types.BlockID{
				start.Add(3): {{1}},
			},
			localOpinion: votes{{1}: abstain},
			config: Config{
				LocalThreshold:  big.NewRat(1, 10),
				GlobalThreshold: big.NewRat(7, 10),
			},
			expected:            start.Add(2),
			expectedTotalWeight: weightFromUint64(20),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			state := newCommonState()
			state.epochWeight = tc.epochWeight
			state.verified = tc.verified
			state.processed = tc.processed
			state.blocks = tc.blocks
			state.localVotes = tc.localOpinion

			v := newVerifying(tc.config, &state)
			v.layerWeights = tc.layersWeight
			v.totalWeight = tc.totalWeight

			require.Equal(t, tc.expected, v.verifyLayers(logger))
			require.Equal(t, tc.expectedTotalWeight.String(), v.totalWeight.String())
		})
	}
}
