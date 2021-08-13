package tortoisebeacon

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestTortoiseBeacon_calcBeacon(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight", mock.AnythingOfType("types.EpochID")).Return(uint64(1), nil, nil)
	mockDB.On("GetNodeAtxIDForEpoch", mock.AnythingOfType("*signing.PublicKey"), mock.AnythingOfType("types.EpochID")).Return(types.ATXID{}, nil)
	mockATXHeader := types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1,
	}
	mockDB.On("GetAtxHeader", mock.AnythingOfType("types.ATXID")).Return(&mockATXHeader, nil)

	const (
		epoch  = 5
		rounds = 3
	)

	types.SetLayersPerEpoch(1)

	tt := []struct {
		name  string
		epoch types.EpochID
		round types.RoundID
		votes allVotes
		hash  types.Hash32
	}{
		{
			name:  "With Cache",
			epoch: epoch,
			votes: allVotes{
				valid: proposalSet{
					"0x1": {},
					"0x2": {},
					"0x4": {},
					"0x5": {},
				},
				invalid: proposalSet{
					"0x3": {},
					"0x6": {},
				},
			},
			hash: types.HexToHash32("0x6d148de54cc5ac334cdf4537018209b0e9f5ea94c049417103065eac777ddb5c"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					RoundsNumber: rounds,
					Theta:        big.NewRat(1, 1),
				},
				lastLayer: types.NewLayerID(epoch),
				Log:       logtest.New(t).WithName("TortoiseBeacon"),
				beacons:   make(map[types.EpochID]types.Hash32),
				atxDB:     mockDB,
			}

			tb.initGenesisBeacons()

			err := tb.calcBeacon(epoch, tc.votes)
			r.NoError(err)
			r.EqualValues(tc.hash.String(), tb.beacons[epoch].String())
		})
	}
}
