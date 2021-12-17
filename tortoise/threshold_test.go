package tortoise

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestUpdateThresholds(t *testing.T) {
	genesis := types.GetEffectiveGenesis()

	for _, tc := range []struct {
		desc                      string
		config                    Config
		processed, last, verified types.LayerID
		mode                      mode
		epochWeight               map[types.EpochID]weight

		expectedLocal  weight
		expectedGlobal weight
	}{
		{
			desc: "WindowIsNotShorterThanProcessed",
			config: Config{
				LocalThreshold:                  big.NewRat(1, 2),
				GlobalThreshold:                 big.NewRat(1, 2),
				VerifyingModeVerificationWindow: 1,
			},
			processed: genesis.Add(4),
			last:      genesis.Add(4),
			verified:  genesis,
			epochWeight: map[types.EpochID]weight{
				2: weightFromUint64(10),
			},
			expectedLocal:  weightFromUint64(5),
			expectedGlobal: weightFromUint64(20),
		},
		{
			desc: "VerifyingLimitIsUsed",
			config: Config{
				LocalThreshold:                  big.NewRat(1, 2),
				GlobalThreshold:                 big.NewRat(1, 2),
				VerifyingModeVerificationWindow: 2,
			},
			processed: genesis.Add(1),
			last:      genesis.Add(4),
			verified:  genesis,
			epochWeight: map[types.EpochID]weight{
				2: weightFromUint64(10),
			},
			expectedLocal:  weightFromUint64(5),
			expectedGlobal: weightFromUint64(15),
		},
		{
			desc: "FullLimitIsUsed",
			config: Config{
				LocalThreshold:             big.NewRat(1, 2),
				GlobalThreshold:            big.NewRat(1, 2),
				FullModeVerificationWindow: 3,
			},
			processed: genesis.Add(1),
			last:      genesis.Add(4),
			verified:  genesis,
			mode:      mode{}.toggleMode(),
			epochWeight: map[types.EpochID]weight{
				2: weightFromUint64(10),
			},
			expectedLocal:  weightFromUint64(5),
			expectedGlobal: weightFromUint64(20),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			state := commonState{
				epochWeight: tc.epochWeight,
				processed:   tc.processed,
				last:        tc.last,
				verified:    tc.verified,
			}
			updateThresholds(logtest.New(t), tc.config, &state, tc.mode)
			require.Equal(t, tc.expectedLocal.String(), state.localThreshold.String())
			require.Equal(t, tc.expectedGlobal.String(), state.globalThreshold.String())
		})
	}
}
