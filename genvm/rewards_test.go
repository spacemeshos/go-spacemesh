package vm

import (
	"testing"

	"github.com/spacemeshos/economics/rewards"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestValidateRewards(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		rewards []types.AnyReward
		err     bool
	}{
		{
			desc: "sanity",
			rewards: []types.AnyReward{
				{
					Coinbase: types.Address{1},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
				{
					Coinbase: types.Address{2},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
				{
					Coinbase: types.Address{3},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
			},
		},
		{
			desc:    "empty",
			rewards: []types.AnyReward{},
			err:     true,
		},
		{
			desc: "nil",
			err:  true,
		},
		{
			desc: "zero num",
			rewards: []types.AnyReward{
				{
					Coinbase: types.Address{1},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
				{
					Coinbase: types.Address{3},
					Weight:   types.RatNum{Num: 0, Denom: 3},
				},
			},
			err: true,
		},
		{
			desc: "zero denom",
			rewards: []types.AnyReward{
				{
					Coinbase: types.Address{1},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
				{
					Coinbase: types.Address{3},
					Weight:   types.RatNum{Num: 1, Denom: 0},
				},
			},
			err: true,
		},
		{
			desc: "multiple per coinbase",
			rewards: []types.AnyReward{
				{
					Coinbase: types.Address{1},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
				{
					Coinbase: types.Address{3},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
				{
					Coinbase: types.Address{1},
					Weight:   types.RatNum{Num: 1, Denom: 3},
				},
			},
			err: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateRewards(tc.rewards)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRewards(t *testing.T) {
	genTester := func(t *testing.T) *tester {
		return newTester(t).
			addSingleSig(10).
			applyGenesis()
	}
	ref := genTester(t)
	const spawnFee = 496
	require.Equal(t, int(spawnFee), ref.estimateSpawnGas(0))
	// this is hardcoded so that you can see which number is divided without reminder
	// and pick correct fractions for tests
	expected := []int{
		477618397593,
		477618296206,
		477618194821,
		477618093434,
		477617992047,
	}
	for i := 0; i < 5; i++ {
		require.Equal(t, expected[i], int(rewards.TotalSubsidyAtLayer(uint32(i))))
	}
	tcs := []templateTestCase{
		{
			desc: "sanity",
			layers: []layertc{
				{
					rewards: []reward{{address: 1, share: 1}},
					expected: map[int]change{
						1: earned{amount: expected[0]},
					},
				},
				{
					rewards: []reward{{address: 2, share: 1}},
					expected: map[int]change{
						2: earned{amount: expected[1]},
					},
				},
				{
					rewards: []reward{{address: 3, share: 1}},
					expected: map[int]change{
						3: earned{amount: expected[2]},
					},
				},
			},
		},
		{
			desc: "empty layer",
			layers: []layertc{
				{
					rewards: []reward{{address: 1, share: 1}},
					expected: map[int]change{
						1: earned{amount: expected[0]},
					},
				},
				{},
				{
					rewards: []reward{{address: 3, share: 1}},
					expected: map[int]change{
						3: earned{amount: expected[2]},
					},
				},
				{},
				{
					rewards: []reward{{address: 5, share: 1}},
					expected: map[int]change{
						5: earned{amount: expected[4]},
					},
				},
			},
		},
		{
			desc: "subsidy rounded down",
			layers: []layertc{
				{
					rewards: []reward{{address: 1, share: 0.5}, {address: 2, share: 0.5}},
					expected: map[int]change{
						1: earned{amount: (expected[0] - 1) / 2},
						2: earned{amount: (expected[0] - 1) / 2},
					},
				},
				{
					rewards: []reward{{address: 1, share: 0.9}, {address: 2, share: 0.1}},
					expected: map[int]change{
						1: earned{amount: expected[1] * 9 / 10},
						2: earned{amount: expected[1] / 10},
					},
				},
			},
		},
		{
			desc: "fees and subsidy rounded down together",
			layers: []layertc{
				{
					txs:     []testTx{&selfSpawnTx{8}},
					rewards: []reward{{address: 1, share: 0.5}, {address: 2, share: 0.5}},
					expected: map[int]change{
						1: earned{amount: (expected[0] - 1 + spawnFee) / 2},
						2: earned{amount: (expected[0] - 1 + spawnFee) / 2},
					},
				},
				{
					txs:     []testTx{&selfSpawnTx{9}},
					rewards: []reward{{address: 1, share: 0.9}, {address: 2, share: 0.1}},
					expected: map[int]change{
						1: earned{amount: (expected[1] + spawnFee) * 9 / 10},
						2: earned{amount: (expected[1] + spawnFee) / 10},
					},
				},
			},
		},
	}
	runTestCases(t, tcs, genTester)
}
