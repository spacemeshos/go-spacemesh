package tortoise

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

func TestComputeThreshold(t *testing.T) {
	genesis := types.GetEffectiveGenesis()

	for _, tc := range []struct {
		desc                      string
		config                    Config
		processed, last, verified types.LayerID
		epochs                    map[types.EpochID]*epochInfo

		expectedGlobal util.Weight
	}{
		{
			desc: "WindowIsNotShorterThanProcessed",
			config: Config{
				LocalThreshold:  big.NewRat(1, 2),
				GlobalThreshold: big.NewRat(1, 2),
				WindowSize:      1,
			},
			processed: genesis.Add(4),
			last:      genesis.Add(4),
			verified:  genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: 10},
			},
			expectedGlobal: util.WeightFromUint64(20),
		},
		{
			desc: "VerifyingLimitIsUsed",
			config: Config{
				LocalThreshold:  big.NewRat(1, 2),
				GlobalThreshold: big.NewRat(1, 2),
				WindowSize:      2,
			},
			processed: genesis.Add(1),
			last:      genesis.Add(4),
			verified:  genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: 10},
			},
			expectedGlobal: util.WeightFromUint64(15),
		},
		{
			desc: "FullLimitIsUsed",
			config: Config{
				LocalThreshold:  big.NewRat(1, 2),
				GlobalThreshold: big.NewRat(1, 2),
				WindowSize:      3,
			},
			processed: genesis.Add(1),
			last:      genesis.Add(4),
			verified:  genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: 10},
			},
			expectedGlobal: util.WeightFromUint64(20),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			global := computeGlobalThreshold(
				tc.config, weight{}, tc.epochs,
				tc.verified.Add(1), tc.last, tc.processed,
			)
			require.Equal(t, tc.expectedGlobal.String(), global.String())
		})
	}
}
