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
		desc         string
		ballots      [][]*ballotInfo
		layerWeights []util.Weight
		total        []util.Weight
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
						weight: ballotWeight,
					},
					{
						id: ballots[1],
						base: baseInfo{
							id: goodbase,
						},
						weight: ballotWeight,
					},
				},
				{
					{
						id: ballots[2],
						base: baseInfo{
							id: goodbase,
						},
						weight: ballotWeight,
					},
				},
			},
			layerWeights: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(10)},
			total:        []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(30)},
		},
		{
			desc: "AllBad",
			ballots: [][]*ballotInfo{
				{
					{
						id:       ballots[0],
						weight:   ballotWeight,
						goodness: conditionBadBeacon,
					},
					{
						id:       ballots[1],
						weight:   ballotWeight,
						goodness: conditionBadBeacon,
					},
				},
				{
					{
						id:     ballots[2],
						weight: ballotWeight,
					},
				},
			},
			layerWeights: []util.Weight{util.WeightFromUint64(0), util.WeightFromUint64(0)},
			total:        []util.Weight{util.WeightFromUint64(0), util.WeightFromUint64(0)},
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
					},
					{
						id:     ballots[1],
						weight: ballotWeight,
						base: baseInfo{
							id: goodbase,
						},
					},
				},
				{
					{
						id:       ballots[2],
						weight:   ballotWeight,
						goodness: conditionBadBeacon,
					},
				},
			},
			layerWeights: []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(0)},
			total:        []util.Weight{util.WeightFromUint64(20), util.WeightFromUint64(20)},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)
			v := newVerifying(Config{}, newState())
			v.ballotRefs[goodbase] = &ballotInfo{
				id: goodbase,
			}

			for i := range tc.ballots {
				lid := start.Add(uint32(i + 1))
				v.countVotes(logger, lid, tc.ballots[i])
				require.Equal(t, tc.layerWeights[i].String(), v.layer(lid).verifying.good.String())
				require.Equal(t, tc.total[i], v.totalGoodWeight)
			}
		})
	}
}

func TestVerifying_Verify(t *testing.T) {
	require.Equal(t, 4, int(types.GetLayersPerEpoch()))
	start := types.GetEffectiveGenesis()
	verified := start
	processed := start.Add(4)
	epochWeight := map[types.EpochID]util.Weight{
		2: util.WeightFromUint64(10),
		3: util.WeightFromUint64(10),
	}
	const localHeight = 100
	referenceHeight := map[types.EpochID]uint64{
		2: localHeight,
		3: localHeight,
	}
	config := Config{
		LocalThreshold:                  big.NewRat(1, 10),
		GlobalThreshold:                 big.NewRat(7, 10),
		VerifyingModeVerificationWindow: 10,
		Hdist:                           10,
		Zdist:                           1,
	}

	for _, tc := range []struct {
		desc        string
		totalWeight util.Weight
		layers      map[types.LayerID]*layerInfo

		expected            types.LayerID
		expectedTotalWeight util.Weight
		expectedValidity    map[types.BlockID]sign
	}{
		{
			desc:        "sanity",
			totalWeight: util.WeightFromUint64(32),
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(6),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
		},
		{
			desc: "with abstained votes",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good:      util.WeightFromUint64(8),
						abstained: util.WeightFromUint64(2),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good:      util.WeightFromUint64(6),
						abstained: util.WeightFromUint64(4),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(34),
			expected:            start.Add(2),
			expectedTotalWeight: util.WeightFromUint64(18),
		},
		{
			desc:        "some crossed threshold",
			totalWeight: util.WeightFromUint64(36),
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(12),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(14),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(2),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(8),
					},
				},
			},
			expected:            start.Add(1),
			expectedTotalWeight: util.WeightFromUint64(24),
		},
		{
			desc: "hare undecided",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: neutral, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(2),
			expectedTotalWeight: util.WeightFromUint64(20),
		},
		{
			desc: "multiple blocks",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
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
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 10, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, height: 20, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, height: 30, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
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
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 30, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, height: 20, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, height: 10, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
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
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 20, layer: start.Add(2)},
						{id: types.BlockID{2}, hare: against, height: localHeight + 1, layer: start.Add(2)},
						{id: types.BlockID{3}, hare: against, height: localHeight + 1, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
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
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: localHeight + 1, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: neutral, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(1),
			expectedTotalWeight: util.WeightFromUint64(30),
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
						good: util.WeightFromUint64(10),
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(40),
			expected:            start.Add(3),
			expectedTotalWeight: util.WeightFromUint64(10),
			expectedValidity:    map[types.BlockID]sign{},
		},
		{
			desc: "some empty layers are not verified",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
				start.Add(2): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(4),
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						good: util.WeightFromUint64(10),
					},
				},
			},
			totalWeight:         util.WeightFromUint64(34),
			expected:            start.Add(1),
			expectedTotalWeight: util.WeightFromUint64(24),
			expectedValidity:    map[types.BlockID]sign{},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)

			state := newState()
			state.epochWeight = epochWeight
			state.referenceHeight = referenceHeight
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

			state.localThreshold, state.globalThreshold = computeThresholds(
				logger, config, mode{},
				state.verified.Add(1), state.processed, state.processed,
				state.epochWeight,
			)

			v := newVerifying(config, state)
			v.totalGoodWeight = tc.totalWeight

			iterateLayers(verified.Add(1), processed.Sub(1), func(lid types.LayerID) bool {
				if !v.verify(logger, lid) {
					return false
				}
				state.verified = lid
				state.localThreshold, state.globalThreshold = computeThresholds(logger, config, mode{},
					state.verified.Add(1), state.processed, state.processed,
					state.epochWeight,
				)
				return true
			})
			require.Equal(t, tc.expected, state.verified)
			require.Equal(t, tc.expectedTotalWeight.String(), v.totalGoodWeight.String())
			for block, sig := range tc.expectedValidity {
				ref, exist := state.blockRefs[block]
				require.True(t, exist, "block %d should be in state", block)
				require.Equal(t, sig, ref.validity)
			}
		})
	}
}
