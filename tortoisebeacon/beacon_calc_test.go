package tortoisebeacon

import (
	"context"
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

	const (
		epoch  = 5
		rounds = 3
	)

	types.SetLayersPerEpoch(1)

	tt := []struct {
		name          string
		epoch         types.EpochID
		round         types.RoundID
		incomingVotes map[types.EpochID]map[types.RoundID]votesPerPK
		votes         votesSetPair
		hash          types.Hash32
	}{
		{
			name:  "With Cache",
			epoch: epoch,
			votes: votesSetPair{
				ValidVotes: hashSet{
					"0x1": {},
					"0x2": {},
					"0x4": {},
					"0x5": {},
				},
				InvalidVotes: hashSet{
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
				lastLayer:     types.NewLayerID(epoch),
				Log:           logtest.New(t).WithName("TortoiseBeacon"),
				incomingVotes: tc.incomingVotes,
				beacons:       make(map[types.EpochID]types.Hash32),
				atxDB:         mockDB,
			}

			tb.initGenesisBeacons()

			err := tb.calcBeacon(context.TODO(), epoch, tc.votes)
			r.NoError(err)
			r.EqualValues(tc.hash.String(), tb.beacons[epoch].String())
		})
	}
}

func TestTortoiseBeacon_calcTortoiseBeaconHashList(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight", mock.AnythingOfType("types.EpochID")).Return(uint64(1), nil, nil)

	const (
		epoch  = 5
		rounds = 3
	)

	tt := []struct {
		name          string
		epoch         types.EpochID
		round         types.RoundID
		incomingVotes map[types.EpochID]map[types.RoundID]votesPerPK
		votes         votesSetPair
		hashes        proposalList
	}{
		{
			name:  "With Cache",
			epoch: epoch,
			votes: votesSetPair{
				ValidVotes: hashSet{
					"0x1": {},
					"0x2": {},
					"0x4": {},
					"0x5": {},
				},
				InvalidVotes: hashSet{
					"0x3": {},
					"0x6": {},
				},
			},
			hashes: proposalList{
				"0x1",
				"0x2",
				"0x4",
				"0x5",
			},
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
				Log:           logtest.New(t).WithName("TortoiseBeacon"),
				incomingVotes: tc.incomingVotes,
				atxDB:         mockDB,
			}

			hashes, err := tb.calcTortoiseBeaconHashList(epoch, tc.votes)
			r.NoError(err)
			r.EqualValues(tc.hashes.Sort(), hashes.Sort())
		})
	}
}
