package tortoise

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestVerifyingDetermineGoodness(t *testing.T) {
	goodbase := types.BallotID{254}
	abstainedBase := types.BallotID{255}
	ballots := []types.BallotID{{1}, {2}, {3}}
	blocks := []types.BlockID{{11}, {22}, {33}}
	for _, tc := range []struct {
		desc string
		commonState
		ballot tortoiseBallot
		expect goodness
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
			expect: canBeGood,
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
				hareOutput: votes{
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
			desc: "AbstainedBallot",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					goodbase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{
					blocks[0]: types.NewLayerID(10),
					blocks[1]: types.NewLayerID(10),
					blocks[2]: types.NewLayerID(10),
				},
				hareOutput: votes{
					blocks[0]: support,
					blocks[1]: against,
					blocks[2]: against,
				},
			},
			ballot: tortoiseBallot{
				id: ballots[0], base: goodbase,
				abstain: map[types.LayerID]struct{}{
					types.NewLayerID(10): {},
				},
			},
			expect: abstained,
		},
		{
			desc: "AbstainedBase",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					abstainedBase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{},
			},
			ballot: tortoiseBallot{
				id:   ballots[0],
				base: abstainedBase,
				abstain: map[types.LayerID]struct{}{
					types.NewLayerID(11): {},
				},
			},
			expect: bad,
		},
		{
			desc: "ConsistentWithAbstainedBase",
			commonState: commonState{
				badBeaconBallots: map[types.BallotID]struct{}{},
				ballotLayer: map[types.BallotID]types.LayerID{
					abstainedBase: types.NewLayerID(10),
				},
				blockLayer: map[types.BlockID]types.LayerID{},
			},
			ballot: tortoiseBallot{
				id:   ballots[0],
				base: abstainedBase,
			},
			expect: canBeGood,
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
					blocks[1]: types.NewLayerID(10),
					blocks[2]: types.NewLayerID(10),
				},
				hareOutput: votes{
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
			expect: good,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			v := newVerifying(Config{}, &tc.commonState)
			v.goodBallots[goodbase] = good
			v.goodBallots[abstainedBase] = abstained
			require.Equal(t, tc.expect.String(), v.determineGoodness(logger, tc.ballot).String())
		})
	}
}

func TestVerifyingProcessLayer(t *testing.T) {
	var (
		goodbase, badbase = types.BallotID{0}, types.BallotID{0, 1}
		ballots           = []types.BallotID{{1}, {2}, {3}, {4}}
		ballotWeight      = util.WeightFromUint64(10)
		blocks            = []types.BlockID{{11}, {22}, {33}}
		start             = types.GetEffectiveGenesis()
	)
	genCommonState := func() commonState {
		state := newCommonState()
		state.hareOutput = votes{
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
		state.ballots = map[types.LayerID][]ballotInfo{
			start.Add(1): {
				{id: ballots[0], weight: ballotWeight},
				{id: ballots[1], weight: ballotWeight},
			},
			start.Add(2): {
				{id: ballots[2], weight: ballotWeight},
				{id: ballots[3], weight: ballotWeight},
			},
		}
		return state
	}

	for _, tc := range []struct {
		desc            string
		ballots         [][]tortoiseBallot
		layerWeights    []util.Weight
		total           []util.Weight
		abstainedWeight []map[types.LayerID]util.Weight
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
			layerWeights: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(10)},
			total:        []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(30)},
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
			layerWeights: []util.Weight{util.WeightFromUint64(0), util.WeightFromUint64(0)},
			total:        []util.Weight{util.WeightFromUint64(0), util.WeightFromUint64(0)},
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
			layerWeights: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(0)},
			total:        []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(20)},
		},
		{
			desc: "AbstainedWeight",
			ballots: [][]tortoiseBallot{
				{
					{
						id: ballots[0], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
						abstain: map[types.LayerID]struct{}{
							start: {},
						},
					},
					{
						id: ballots[1], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
						abstain: map[types.LayerID]struct{}{
							start: {},
						},
					},
				},
				{
					{
						id: ballots[3], base: goodbase, weight: ballotWeight,
						votes: votes{blocks[0]: support},
						abstain: map[types.LayerID]struct{}{
							start.Add(1): {},
						},
					},
				},
			},
			layerWeights: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(10)},
			total:        []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(30)},
			abstainedWeight: []map[types.LayerID]util.Weight{
				{
					start: util.WeightFromFloat64(20),
				},
				{
					start:        util.WeightFromFloat64(20),
					start.Add(1): util.WeightFromFloat64(10),
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			state := genCommonState()
			v := newVerifying(Config{}, &state)
			v.goodBallots[goodbase] = good

			for i := range tc.ballots {
				lid := start.Add(uint32(i + 1))
				v.countVotes(logger, lid, tc.ballots[i])
				require.Equal(t, tc.layerWeights[i], v.goodWeight[lid])
				require.Equal(t, tc.total[i], v.totalGoodWeight)
				if tc.abstainedWeight != nil {
					require.Equal(t, tc.abstainedWeight[i], v.abstainedWeight)
				}
			}
		})
	}
}

func TestVerifying_Verify(t *testing.T) {
	require.Equal(t, 4, int(types.GetLayersPerEpoch()))
	start := types.NewLayerID(0)
	for _, tc := range []struct {
		desc                string
		epochWeight         map[types.EpochID]util.Weight
		layersWeight        map[types.LayerID]util.Weight
		abstainedWeight     map[types.LayerID]util.Weight
		totalWeight         util.Weight
		verified, processed types.LayerID
		blocks              map[types.LayerID][]blockInfo
		localOpinion        votes
		config              Config

		expected            types.LayerID
		expectedTotalWeight util.Weight
	}{
		{
			desc: "All",
			epochWeight: map[types.EpochID]util.Weight{
				0: util.WeightFromUint64(10),
				1: util.WeightFromUint64(10),
			},
			layersWeight: map[types.LayerID]util.Weight{
				start.Add(1): util.WeightFromUint64(8),
				start.Add(2): util.WeightFromUint64(8),
				start.Add(3): util.WeightFromUint64(6),
				start.Add(4): util.WeightFromUint64(10),
			},
			abstainedWeight: map[types.LayerID]util.Weight{},
			totalWeight:     util.WeightFromUint64(32),
			verified:        start,
			processed:       start.Add(4),
			blocks:          map[types.LayerID][]blockInfo{},
			localOpinion:    votes{},
			config: Config{
				LocalThreshold:                  big.NewRat(1, 10),
				GlobalThreshold:                 big.NewRat(7, 10),
				VerifyingModeVerificationWindow: 10,
			},
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
		},
		{
			desc: "Abstained",
			epochWeight: map[types.EpochID]util.Weight{
				0: util.WeightFromUint64(10),
				1: util.WeightFromUint64(10),
			},
			layersWeight: map[types.LayerID]util.Weight{
				start.Add(1): util.WeightFromUint64(8),
				start.Add(2): util.WeightFromUint64(8),
				start.Add(3): util.WeightFromUint64(6),
				start.Add(4): util.WeightFromUint64(10),
			},
			abstainedWeight: map[types.LayerID]util.Weight{
				start.Add(2): util.WeightFromUint64(2),
				start.Add(3): util.WeightFromUint64(4),
			},
			totalWeight:  util.WeightFromUint64(34),
			verified:     start,
			processed:    start.Add(4),
			blocks:       map[types.LayerID][]blockInfo{},
			localOpinion: votes{},
			config: Config{
				LocalThreshold:                  big.NewRat(1, 10),
				GlobalThreshold:                 big.NewRat(7, 10),
				VerifyingModeVerificationWindow: 10,
			},
			expected:            start.Add(2),
			expectedTotalWeight: util.WeightFromUint64(18),
		},
		{
			desc: "Some",
			epochWeight: map[types.EpochID]util.Weight{
				0: util.WeightFromUint64(10),
				1: util.WeightFromUint64(10),
			},
			layersWeight: map[types.LayerID]util.Weight{
				start.Add(1): util.WeightFromUint64(12),
				start.Add(2): util.WeightFromUint64(14),
				start.Add(3): util.WeightFromUint64(2),
				start.Add(4): util.WeightFromUint64(8),
			},
			abstainedWeight: map[types.LayerID]util.Weight{},
			totalWeight:     util.WeightFromUint64(36),
			verified:        start,
			processed:       start.Add(4),
			config: Config{
				LocalThreshold:                  big.NewRat(1, 10),
				GlobalThreshold:                 big.NewRat(7, 10),
				VerifyingModeVerificationWindow: 10,
			},
			expected:            start.Add(1),
			expectedTotalWeight: util.WeightFromUint64(24),
		},
		{
			desc: "Undecided",
			epochWeight: map[types.EpochID]util.Weight{
				0: util.WeightFromUint64(10),
				1: util.WeightFromUint64(10),
			},
			layersWeight: map[types.LayerID]util.Weight{
				start.Add(1): util.WeightFromUint64(10),
				start.Add(2): util.WeightFromUint64(10),
				start.Add(3): util.WeightFromUint64(10),
				start.Add(4): util.WeightFromUint64(10),
			},
			abstainedWeight: map[types.LayerID]util.Weight{},
			totalWeight:     util.WeightFromUint64(40),
			verified:        start,
			processed:       start.Add(4),
			blocks: map[types.LayerID][]blockInfo{
				start.Add(3): {{id: types.BlockID{1}}},
			},
			localOpinion: votes{{1}: abstain},
			config: Config{
				LocalThreshold:                  big.NewRat(1, 10),
				GlobalThreshold:                 big.NewRat(7, 10),
				VerifyingModeVerificationWindow: 10,
			},
			expected:            start.Add(2),
			expectedTotalWeight: util.WeightFromUint64(20),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			state := newCommonState()
			state.epochWeight = tc.epochWeight
			state.verified = tc.verified
			state.processed = tc.processed
			state.last = tc.processed
			state.blocks = tc.blocks
			state.hareOutput = tc.localOpinion

			state.localThreshold, state.globalThreshold = computeThresholds(logger, tc.config, mode{},
				state.verified.Add(1), state.processed, state.processed,
				state.epochWeight,
			)

			v := newVerifying(tc.config, &state)
			v.goodWeight = tc.layersWeight
			v.totalGoodWeight = tc.totalWeight
			v.abstainedWeight = tc.abstainedWeight
			iterateLayers(tc.verified.Add(1), tc.processed.Sub(1), func(lid types.LayerID) bool {
				if !v.verify(logger, lid) {
					return false
				}
				state.verified = lid
				state.localThreshold, state.globalThreshold = computeThresholds(logger, tc.config, mode{},
					state.verified.Add(1), state.processed, state.processed,
					state.epochWeight,
				)
				return true
			})
			require.Equal(t, tc.expected, state.verified)
			require.Equal(t, tc.expectedTotalWeight.String(), v.totalGoodWeight.String())
		})
	}
}
