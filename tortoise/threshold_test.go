package tortoise

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestComputeThreshold(t *testing.T) {
	genesis := types.GetEffectiveGenesis()

	for _, tc := range []struct {
		desc                      string
		config                    Config
		processed, last, verified types.LayerID
		mode                      mode
		epochWeight               map[types.EpochID]util.Weight

		expectedLocal  util.Weight
		expectedGlobal util.Weight
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
			epochWeight: map[types.EpochID]util.Weight{
				2: util.WeightFromUint64(10),
			},
			expectedLocal:  util.WeightFromUint64(5),
			expectedGlobal: util.WeightFromUint64(20),
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
			epochWeight: map[types.EpochID]util.Weight{
				2: util.WeightFromUint64(10),
			},
			expectedLocal:  util.WeightFromUint64(5),
			expectedGlobal: util.WeightFromUint64(15),
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
			epochWeight: map[types.EpochID]util.Weight{
				2: util.WeightFromUint64(10),
			},
			expectedLocal:  util.WeightFromUint64(5),
			expectedGlobal: util.WeightFromUint64(20),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			local, global := computeThresholds(
				logtest.New(t), tc.config, tc.mode,
				tc.verified.Add(1), tc.last, tc.processed, tc.epochWeight,
			)
			require.Equal(t, tc.expectedLocal.String(), local.String())
			require.Equal(t, tc.expectedGlobal.String(), global.String())
		})
	}
}

func TestReferenceHeight(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		epoch    int
		heights  []int
		expected int
	}{
		{
			desc:  "no atxs",
			epoch: 1,
		},
		{
			desc:     "one",
			epoch:    1,
			heights:  []int{10},
			expected: 10,
		},
		{
			desc:     "two",
			epoch:    2,
			heights:  []int{10, 20},
			expected: 15,
		},
		{
			desc:     "median odd",
			epoch:    3,
			heights:  []int{30, 10, 20},
			expected: 20,
		},
		{
			desc:     "median even",
			epoch:    4,
			heights:  []int{30, 20, 10, 40},
			expected: 25,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
			for i, height := range tc.heights {
				atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
					NIPostChallenge: types.NIPostChallenge{
						PubLayerID: (types.EpochID(tc.epoch) - 1).FirstLayer(),
					},
				}}
				atx.SetID(&types.ATXID{byte(i + 1)})
				atx.Verify(0, uint64(height))
				activation.SignAtx(signing.NewEdSigner(), atx)
				require.NoError(t, atx.CalcAndSetNodeID())
				require.NoError(t, atxs.Add(cdb, atx, time.Time{}))
			}
			_, height, err := extractAtxsData(cdb, types.EpochID(tc.epoch))
			require.NoError(t, err)
			require.Equal(t, tc.expected, int(height))
		})
	}
}
