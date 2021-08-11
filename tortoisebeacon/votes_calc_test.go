package tortoisebeacon

import (
	"math/big"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func coinValueMock(tb testing.TB, value bool) coin {
	ctrl := gomock.NewController(tb)
	coinMock := mocks.NewMockcoin(ctrl)
	coinMock.EXPECT().StartEpoch(
		gomock.AssignableToTypeOf(types.EpochID(0)),
		gomock.AssignableToTypeOf(weakcoin.UnitAllowances{}),
	).AnyTimes()
	coinMock.EXPECT().FinishEpoch().AnyTimes()
	coinMock.EXPECT().StartRound(gomock.Any(), gomock.AssignableToTypeOf(types.RoundID(0))).
		AnyTimes().Return(nil)
	coinMock.EXPECT().FinishRound().AnyTimes()
	coinMock.EXPECT().Get(
		gomock.AssignableToTypeOf(types.EpochID(0)),
		gomock.AssignableToTypeOf(types.RoundID(0)),
	).AnyTimes().Return(value)
	return coinMock
}

func TestTortoiseBeacon_calcVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight",
		mock.AnythingOfType("types.EpochID")).
		Return(uint64(1), nil, nil)

	const epoch = 5
	const round = 3

	tt := []struct {
		name          string
		epoch         types.EpochID
		round         types.RoundID
		votesMargin   votesMarginMap
		incomingVotes []map[nodeID]votesSetPair
		expected      votesSetPair
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			round: round,
			votesMargin: map[proposal]int{
				"0x1": 2,
				"0x2": 0,
				"0x3": 0,
				"0x4": 1,
				"0x5": 0,
				"0x6": 0,
			},
			expected: votesSetPair{
				ValidVotes: hashSet{
					"0x1": struct{}{},
					"0x4": struct{}{},
				},
				InvalidVotes: hashSet{
					"0x2": struct{}{},
					"0x3": struct{}{},
					"0x5": struct{}{},
					"0x6": struct{}{},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					Theta: big.NewRat(1, 1),
				},
				Log:         logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:       mockDB,
				votesMargin: tc.votesMargin,
			}

			result, err := tb.calcVotes(tc.epoch, tc.round, false)
			r.NoError(err)
			r.EqualValues(tc.expected, result)
		})
	}
}

func TestTortoiseBeacon_calcOwnCurrentRoundVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	const threshold = 3

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight",
		mock.AnythingOfType("types.EpochID")).
		Return(uint64(threshold), nil, nil)

	tt := []struct {
		name               string
		epoch              types.EpochID
		round              types.RoundID
		ownFirstRoundVotes votesSetPair
		votesCount         votesMarginMap
		weakCoin           bool
		result             votesSetPair
	}{
		{
			name:  "Case 1",
			epoch: 5,
			round: 5,
			ownFirstRoundVotes: votesSetPair{
				ValidVotes: hashSet{
					"0x1": {},
					"0x2": {},
				},
				InvalidVotes: hashSet{
					"0x3": {},
				},
			},
			votesCount: votesMarginMap{
				"0x1": threshold * 2,
				"0x2": -threshold * 3,
				"0x3": threshold / 2,
			},
			weakCoin: true,
			result: votesSetPair{
				ValidVotes: hashSet{
					"0x1": {},
					"0x3": {},
				},
				InvalidVotes: hashSet{
					"0x2": {},
				},
			},
		},
		{
			name:  "Case 2",
			epoch: 5,
			round: 5,
			votesCount: votesMarginMap{
				"0x1": threshold * 2,
				"0x2": -threshold * 3,
				"0x3": threshold / 2,
			},
			weakCoin: false,
			result: votesSetPair{
				ValidVotes: hashSet{
					"0x1": {},
				},
				InvalidVotes: hashSet{
					"0x2": {},
					"0x3": {},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					Theta: big.NewRat(1, 1),
				},
				Log:         logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:       mockDB,
				votesMargin: tc.votesCount,
			}

			result, err := tb.calcOwnCurrentRoundVotes(tc.epoch, tc.round, tc.weakCoin)
			r.NoError(err)
			r.EqualValues(tc.result, result)
		})
	}
}
