package tortoise

import (
	"testing"

	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestVerifyingProcessLayer(t *testing.T) {
	var (
		ballots      = []types.BallotID{{1}, {2}, {3}, {4}}
		ballotWeight = fixed.From(10)
		start        = types.GetEffectiveGenesis()
	)

	for _, tc := range []struct {
		desc      string
		ballots   [][]*ballotInfo
		uncounted []weight
		total     []weight
	}{
		{
			desc: "AllGood",
			ballots: [][]*ballotInfo{
				{
					{
						id:        ballots[0],
						reference: &referenceInfo{},
						weight:    ballotWeight,
					},
					{
						id:        ballots[1],
						reference: &referenceInfo{},
						weight:    ballotWeight,
					},
				},
				{
					{
						id:        ballots[2],
						reference: &referenceInfo{},
						weight:    ballotWeight,
					},
				},
			},
			uncounted: []weight{fixed.From(20), fixed.From(30)},
			total:     []weight{fixed.From(20), fixed.From(30)},
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
						id:     ballots[2],
						weight: ballotWeight,
						conditions: conditions{
							badBeacon: true,
						},
						reference: &referenceInfo{},
					},
				},
			},
			uncounted: []weight{fixed.From(0), fixed.From(0)},
			total:     []weight{fixed.From(0), fixed.From(0)},
		},
		{
			desc: "GoodInFirstLayer",
			ballots: [][]*ballotInfo{
				{
					{
						id:        ballots[0],
						weight:    ballotWeight,
						reference: &referenceInfo{},
					},
					{
						id:        ballots[1],
						weight:    ballotWeight,
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
			uncounted: []weight{fixed.From(20), fixed.From(20)},
			total:     []weight{fixed.From(20), fixed.From(20)},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.Zap(t)
			v := newVerifying(Config{}, newState(atxsdata.New()))
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
		2: {weight: fixed.From(40), height: localHeight},
		3: {weight: fixed.From(40), height: localHeight},
	}

	config := Config{
		WindowSize: 10,
		Hdist:      10,
		Zdist:      1,
	}

	for _, tc := range []struct {
		desc        string
		totalWeight weight
		layers      map[types.LayerID]*layerInfo

		expected         types.LayerID
		expectedValidity map[types.BlockID]sign
	}{
		{
			desc:        "sanity",
			totalWeight: fixed.From(32),
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(8),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(6),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
				},
			},
			expected: start.Add(3),
		},
		{
			desc:        "some crossed threshold",
			totalWeight: fixed.From(36),
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(12),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(26),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(28),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(36),
					},
				},
			},
			expected: start.Add(1),
		},
		{
			desc: "hare undecided",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{2}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{3}, hare: neutral, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight: fixed.From(40),
			expected:    start.Add(2),
		},
		{
			desc: "multiple blocks",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight: fixed.From(40),
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
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 10, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, height: 20, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, height: 30, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight: fixed.From(40),
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
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 30, layer: start.Add(1)},
						{id: types.BlockID{2}, hare: against, height: 20, layer: start.Add(1)},
						{id: types.BlockID{3}, hare: against, height: 10, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight: fixed.From(40),
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
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: 20, layer: start.Add(2)},
						{id: types.BlockID{2}, hare: against, height: localHeight + 1, layer: start.Add(2)},
						{id: types.BlockID{3}, hare: against, height: localHeight + 1, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: support, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight: fixed.From(40),
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
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{4}, hare: support, layer: start.Add(1)},
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{1}, hare: support, height: localHeight + 1, layer: start.Add(2)},
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
					blocks: []*blockInfo{
						{id: types.BlockID{5}, hare: neutral, layer: start.Add(3)},
					},
				},
				start.Add(4): {
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight: fixed.From(40),
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
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(10),
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(20),
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(30),
					},
				},
				start.Add(4): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(40),
					},
				},
			},
			totalWeight:      fixed.From(40),
			expected:         start.Add(3),
			expectedValidity: map[types.BlockID]sign{},
		},
		{
			desc: "some empty layers are not verified",
			layers: map[types.LayerID]*layerInfo{
				start.Add(1): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(13),
					},
				},
				start.Add(2): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(26),
					},
				},
				start.Add(3): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(28),
					},
				},
				start.Add(4): {
					hareTerminated: true,
					verifying: verifyingInfo{
						goodUncounted: fixed.From(34),
					},
				},
			},
			totalWeight:      fixed.From(34),
			expected:         start.Add(1),
			expectedValidity: map[types.BlockID]sign{},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.Zap(t)

			state := newState(atxsdata.New())
			state.epochs = epochs
			state.verified = verified
			state.processed = processed
			state.last = processed
			for lid, info := range tc.layers {
				layer := state.layers.get(state.evicted, lid)
				*layer = *info
			}
			refs := map[types.BlockID]*blockInfo{}
			for _, layer := range state.layers.data {
				for _, block := range layer.blocks {
					refs[block.id] = block
					state.updateRefHeight(layer, block)
				}
			}

			v := newVerifying(config, state)
			v.totalGoodWeight = tc.totalWeight

			iterateLayers(verified.Add(1), processed.Sub(1), func(lid types.LayerID) bool {
				v, _ := v.verify(logger, lid)
				if !v {
					return false
				}
				state.verified = lid
				return true
			})
			require.Equal(t, tc.expected, state.verified)
			for block, sig := range tc.expectedValidity {
				ref, exist := refs[block]
				require.True(t, exist, "block %d should be in state", block)
				require.Equal(t, sig, ref.validity)
			}
		})
	}
}

func iterateLayers(from, to types.LayerID, callback func(types.LayerID) bool) {
	for lid := from; !lid.After(to); lid = lid.Add(1) {
		if !callback(lid) {
			return
		}
	}
}
