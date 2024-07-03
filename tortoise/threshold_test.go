package tortoise

import (
	"testing"
	"time"

	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestComputeThreshold(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	length := types.GetLayersPerEpoch()
	for _, tc := range []struct {
		desc                    string
		config                  Config
		processed, last, target types.LayerID
		epochs                  map[types.EpochID]*epochInfo

		expectedGlobal weight
	}{
		{
			desc:      "sanity",
			processed: genesis.Add(length),
			last:      genesis.Add(length),
			target:    genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(45)},
			},
			expectedGlobal: fixed.New(15),
		},
		{
			desc:      "shorter than epoch",
			processed: genesis.Add(length / 2),
			last:      genesis.Add(length / 2),
			target:    genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(45)},
			},
			expectedGlobal: fixed.From(7.5),
		},
		{
			desc:      "multi epoch",
			processed: genesis.Add(length * 3),
			last:      genesis.Add(length * 3),
			target:    genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(40)},
				3: {weight: fixed.From(40)},
				4: {weight: fixed.From(40)},
			},
			expectedGlobal: fixed.New(40),
		},
		{
			desc:      "not full epoch",
			processed: genesis.Add(length - 1),
			last:      genesis.Add(length - 1),
			target:    genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(40)},
			},
			expectedGlobal: fixed.New(10),
		},
		{
			desc:      "multiple not full epochs",
			processed: genesis.Add(length*2 - 2),
			last:      genesis.Add(length*2 - 2),
			target:    genesis.Add(1),
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(60)},
				3: {weight: fixed.From(60)},
			},
			expectedGlobal: fixed.New(25),
		},
		{
			desc: "window size",
			config: Config{
				WindowSize: 2,
			},
			last:   genesis.Add(length),
			target: genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(45)},
			},
			expectedGlobal: fixed.From(7.5),
		},
		{
			desc: "window size is ignored if processed is past window",
			config: Config{
				WindowSize: 2,
			},
			last:      genesis.Add(length),
			processed: genesis.Add(length - 1),
			target:    genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(45)},
			},
			expectedGlobal: fixed.From(11.25),
		},
		{
			desc: "historical window size based on the target layer",
			config: Config{
				WindowSize: 2,
				HistoricalWindowSize: []WindowSizeInterval{
					{End: genesis.Add(length), Window: 4},
				},
			},
			last:   genesis.Add(2 * length),
			target: genesis,
			epochs: map[types.EpochID]*epochInfo{
				2: {weight: fixed.From(45)},
				3: {weight: fixed.From(45)},
			},
			expectedGlobal: fixed.From(15),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			global := computeGlobalThreshold(
				tc.config, weight{}, tc.epochs,
				tc.target, tc.processed, tc.last,
			)
			require.Equal(t, tc.expectedGlobal.String(), global.String())
		})
	}
}

func TestReferenceHeight(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		epoch    int
		heights  []uint64
		expected int
	}{
		{
			desc:  "no atxs",
			epoch: 1,
		},
		{
			desc:     "one",
			epoch:    1,
			heights:  []uint64{10},
			expected: 10,
		},
		{
			desc:     "two",
			epoch:    2,
			heights:  []uint64{10, 20},
			expected: 15,
		},
		{
			desc:     "median odd",
			epoch:    3,
			heights:  []uint64{30, 10, 20},
			expected: 20,
		},
		{
			desc:     "median even",
			epoch:    4,
			heights:  []uint64{30, 20, 10, 40},
			expected: 25,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			for i, height := range tc.heights {
				atx := &types.ActivationTx{
					PublishEpoch: types.EpochID(tc.epoch) - 1,
					NumUnits:     2,
					TickCount:    height,
				}
				atx.SetID(types.ATXID{byte(i + 1)})
				atx.SetReceived(time.Now())
				require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
			}
			_, height, err := extractAtxsData(db, types.EpochID(tc.epoch))
			require.NoError(t, err)
			require.Equal(t, tc.expected, int(height))
		})
	}
}
