package tortoisebeacon

import (
	"math/big"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
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

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(uint64(1), nil, nil).AnyTimes()
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1,
	}, nil).AnyTimes()

	const epoch = 5
	const round = 3

	tt := []struct {
		name          string
		epoch         types.EpochID
		round         types.RoundID
		votesMargin   map[string]*big.Int
		incomingVotes []map[string]allVotes
		expected      allVotes
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			round: round,
			votesMargin: map[string]*big.Int{
				"0x1": big.NewInt(2),
				"0x2": big.NewInt(0),
				"0x3": big.NewInt(0),
				"0x4": big.NewInt(1),
				"0x5": big.NewInt(0),
				"0x6": big.NewInt(0),
			},
			expected: allVotes{
				valid: proposalSet{
					"0x1": {},
					"0x4": {},
				},
				invalid: proposalSet{
					"0x2": {},
					"0x3": {},
					"0x5": {},
					"0x6": {},
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

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(uint64(threshold), nil, nil).AnyTimes()
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1,
	}, nil).AnyTimes()

	tt := []struct {
		name               string
		epoch              types.EpochID
		round              types.RoundID
		ownFirstRoundVotes allVotes
		votesCount         map[string]*big.Int
		weakCoin           bool
		result             allVotes
	}{
		{
			name:  "Case 1",
			epoch: 5,
			round: 5,
			ownFirstRoundVotes: allVotes{
				valid: proposalSet{
					"0x1": {},
					"0x2": {},
				},
				invalid: proposalSet{
					"0x3": {},
				},
			},
			votesCount: map[string]*big.Int{
				"0x1": big.NewInt(threshold * 2),
				"0x2": big.NewInt(-threshold * 3),
				"0x3": big.NewInt(threshold / 2),
			},
			weakCoin: true,
			result: allVotes{
				valid: proposalSet{
					"0x1": {},
					"0x3": {},
				},
				invalid: proposalSet{
					"0x2": {},
				},
			},
		},
		{
			name:  "Case 2",
			epoch: 5,
			round: 5,
			votesCount: map[string]*big.Int{
				"0x1": big.NewInt(threshold * 2),
				"0x2": big.NewInt(-threshold * 3),
				"0x3": big.NewInt(threshold / 2),
			},
			weakCoin: false,
			result: allVotes{
				valid: proposalSet{
					"0x1": {},
				},
				invalid: proposalSet{
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

			result, err := tb.calcOwnCurrentRoundVotes(tc.epoch, tc.weakCoin)
			r.NoError(err)
			r.EqualValues(tc.result, result)
		})
	}
}
