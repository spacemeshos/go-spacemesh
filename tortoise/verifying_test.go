package tortoise

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestVerifyingProcessLayer(t *testing.T) {
	var (
		goodbase     = types.BallotID{9, 9, 9}
		ballots      = []types.BallotID{{1}, {2}, {3}, {4}}
		ballotWeight = util.WeightFromUint64(10)
		start        = types.GetEffectiveGenesis()
	)

	for _, tc := range []struct {
		desc      string
		ballots   [][]*ballotInfo
		uncounted []util.Weight
		total     []util.Weight
	}{
		{
			desc: "AllGood",
			ballots: [][]*ballotInfo{
				{
					{
						id: ballots[0],
						base: baseInfo{
							id: goodbase,
						},
						reference: &referenceInfo{},
						weight:    ballotWeight,
					},
					{
						id: ballots[1],
						base: baseInfo{
							id: goodbase,
						},
						reference: &referenceInfo{},
						weight:    ballotWeight,
					},
				},
				{
					{
						id: ballots[2],
						base: baseInfo{
							id: goodbase,
						},
						reference: &referenceInfo{},
						weight:    ballotWeight,
					},
				},
			},
			uncounted: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(30)},
			total:     []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(30)},
		},
		{
			desc: "AllBad",
			ballots: [][]*ballotInfo{
				{
					{
						id:     ballots[0],
						weight: ballotWeight,
						conditions: conditions{
							badBeacon: true,
						},
						reference: &referenceInfo{},
					},
					{
						id:     ballots[1],
						weight: ballotWeight,
						conditions: conditions{
							badBeacon: true,
						},
						reference: &referenceInfo{},
					},
				},
				{
					{
						id:        ballots[2],
						weight:    ballotWeight,
						reference: &referenceInfo{},
					},
				},
			},
			uncounted: []util.Weight{util.WeightFromUint64(0), util.WeightFromUint64(0)},
			total:     []util.Weight{util.WeightFromUint64(0), util.WeightFromUint64(0)},
		},
		{
			desc: "GoodInFirstLayer",
			ballots: [][]*ballotInfo{
				{
					{
						id:     ballots[0],
						weight: ballotWeight,
						base: baseInfo{
							id: goodbase,
						},
						reference: &referenceInfo{},
					},
					{
						id:     ballots[1],
						weight: ballotWeight,
						base: baseInfo{
							id: goodbase,
						},
						reference: &referenceInfo{},
					},
				},
				{
					{
						id:     ballots[2],
						weight: ballotWeight,
						conditions: conditions{
							badBeacon: true,
						},
						reference: &referenceInfo{},
					},
				},
			},
			uncounted: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(20)},
			total:     []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(20)},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)
			v := newVerifying(Config{}, newState())
			v.ballotRefs[goodbase] = &ballotInfo{
				id: goodbase,
				conditions: conditions{
					baseGood:   true,
					consistent: true,
				},
			}
			v.processed = start.Add(1).Add(uint32(len(tc.ballots)))

			for i := range tc.ballots {
				lid := start.Add(uint32(i + 1))
				for _, ballot := range tc.ballots[i] {
					ballot.layer = lid
					v.countBallot(logger, ballot)
				}
				require.Equal(t, tc.uncounted[i].String(), v.layer(lid).verifying.goodUncounted.String())
				require.Equal(t, tc.total[i].String(), v.totalGoodWeight.String())
			}
		})
	}
}

func TestVerifying_Verify(t *testing.T) {
	require.Equal(t, 4, int(types.GetLayersPerEpoch()))
	start := types.GetEffectiveGenesis()
	verified := start
	processed := start.Add(4)

	const localHeight = 100
	epochs := map[types.EpochID]*epochInfo{
		2: {weight: 40, height: localHeight},
		3: {weight: 40, height: localHeight},
	}

	config := Config{
		LocalThreshold:  big.NewRat(1, 10),
		GlobalThreshold: big.NewRat(7, 10),
		WindowSize:      10,
		Hdist:           10,
		Zdist:           1,
	}

	for _, tc := range []struct {
		desc        string
		totalWeight util.Weight
		layers      map[types.LayerID]*layerInfo

		expected         types.LayerID
		expectedValidity map[types.BlockID]sign
	}{
		{
			desc:        "sanity",
			totalWeight: util.WeightFromUint64(32),
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(6),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
				},
			},
			expected: start.Add(3),
		},
		{
			desc: "with abstained votes",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(16),
						abstained:     util.WeightFromUint64(2),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(22),
						abstained:     util.WeightFromUint64(5),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(34),
					},
				},
			},
			totalWeight: util.WeightFromUint64(34),
			expected:    start.Add(2),
		},
		{
			desc:        "some crossed threshold",
			totalWeight: util.WeightFromUint64(36),
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(12),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(26),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(28),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(36),
					},
				},
			},
			expected: start.Add(1),
		},
		{
			desc: "hare undecided",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: neutral, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight: util.WeightFromUint64(40),
			expected:    start.Add(2),
		},
		{
			desc: "multiple blocks",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight: util.WeightFromUint64(40),
			expected:    start.Add(3),
			expectedValidity: map[types.BlockID]sign{
				{1}: support,
				{2}: against,
				{3}: against,
				{4}: support,
				{5}: support,
			},
		},
		{
			desc: "multiple blocks hare lowest",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 10, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, height: 20, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, height: 30, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight: util.WeightFromUint64(40),
			expected:    start.Add(3),
			expectedValidity: map[types.BlockID]sign{
				{1}: support,
				{2}: against,
				{3}: against,
				{4}: support,
				{5}: support,
			},
		},
		{
			desc: "multiple blocks hare highest",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 30, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, height: 20, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, height: 10, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight: util.WeightFromUint64(40),
			expected:    start.Add(3),
			expectedValidity: map[types.BlockID]sign{
				{1}: support,
				{2}: against,
				{3}: against,
				{4}: support,
				{5}: support,
			},
		},
		{
			desc: "multiple blocks higher than local height",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 20, layer: start.Add(2)},
						{id: types.BlockID{2}, hare: against, height: localHeight + 1, layer: start.Add(2)},
						{id: types.BlockID{3}, hare: against, height: localHeight + 1, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight: util.WeightFromUint64(40),
			expected:    start.Add(3),
			expectedValidity: map[types.BlockID]sign{
				{1}: support,
				{2}: against,
				{3}: against,
				{4}: support,
				{5}: support,
			},
		},
		{
			desc: "hare output higher than local height",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: localHeight + 1, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: neutral, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight: util.WeightFromUint64(40),
			expected:    start.Add(1),
			expectedValidity: map[types.BlockID]sign{
				{1}: abstain,
				{4}: support,
			},
		},
		{
			desc: "all empty layers",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(30),
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(40),
					},
				},
			},
			totalWeight:      util.WeightFromUint64(40),
			expected:         start.Add(3),
			expectedValidity: map[types.BlockID]sign{},
		},
		{
			desc: "some empty layers are not verified",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(10),
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(20),
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(24),
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: util.WeightFromUint64(34),
					},
				},
			},
			totalWeight:      util.WeightFromUint64(34),
			expected:         start.Add(1),
			expectedValidity: map[types.BlockID]sign{},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			state := newState()
			state.epochs = epochs
			state.verified = verified
			state.processed = processed
			state.last = processed
			state.layers = tc.layers
			for _, layer := range state.layers {
				for _, block := range layer.blocks {
					state.blockRefs[block.id] = block
					state.updateRefHeight(layer, block)
				}
			}

			v := newVerifying(config, state)
			v.totalGoodWeight = tc.totalWeight

			iterateLayers(verified.Add(1), processed.Sub(1), func(lid types.LayerID) bool {
				if !v.verify(logger, lid) {
					return false
				}
				state.verified = lid
				return true
			})
			require.Equal(t, tc.expected, state.verified)
			for block, sig := range tc.expectedValidity {
				ref, exist := state.blockRefs[block]
				require.True(t, exist, "block %d should be in state", block)
				require.Equal(t, sig, ref.validity)
			}
		})
	}
}
