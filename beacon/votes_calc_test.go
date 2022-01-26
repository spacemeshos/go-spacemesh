package beacon

import (
	"context"
	"math/big"
	"sort"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/beacon/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestBeacon_calcVotes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
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
		undecided     []string
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
				"0x6": big.NewInt(-2),
			},
			expected: allVotes{
				valid: proposalSet{
					"0x1": {},
					"0x4": {},
				},
				invalid: proposalSet{
					"0x6": {},
				},
			},
			undecided: []string{"0x2", "0x3", "0x5"},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pd := ProtocolDriver{
				theta:       new(big.Float).SetRat(big.NewRat(1, 1)),
				logger:      logtest.New(t).WithName("Beacon"),
				atxDB:       mockDB,
				votesMargin: tc.votesMargin,
				epochWeight: uint64(1),
			}

			result, undecided, err := pd.calcVotes(context.TODO(), tc.epoch, tc.round)
			require.NoError(t, err)
			sort.Strings(undecided)
			require.Equal(t, tc.undecided, undecided)
			require.EqualValues(t, tc.expected, result)
		})
	}
}

func TestBeacon_calcOwnCurrentRoundVotes(t *testing.T) {
	t.Parallel()

	const threshold = 3

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
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
		result             allVotes
		undecided          []string
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
			result: allVotes{
				valid: proposalSet{
					"0x1": {},
				},
				invalid: proposalSet{
					"0x2": {},
				},
			},
			undecided: []string{"0x3"},
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
			result: allVotes{
				valid: proposalSet{
					"0x1": {},
				},
				invalid: proposalSet{
					"0x2": {},
				},
			},
			undecided: []string{"0x3"},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pd := ProtocolDriver{
				theta:       new(big.Float).SetRat(big.NewRat(1, 1)),
				logger:      logtest.New(t).WithName("Beacon"),
				atxDB:       mockDB,
				votesMargin: tc.votesCount,
				epochWeight: uint64(threshold),
			}

			result, undecided, err := pd.calcOwnCurrentRoundVotes()
			require.NoError(t, err)
			sort.Strings(undecided)
			require.Equal(t, tc.undecided, undecided)
			require.EqualValues(t, tc.result, result)
		})
	}
}

func TestTallyUndecided(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc      string
		expected  allVotes
		undecided []string
		coinFlip  bool
	}{
		{
			desc: "Valid",
			expected: allVotes{
				valid: proposalSet{
					"1": struct{}{},
					"2": struct{}{},
				},
				invalid: proposalSet{},
			},
			undecided: []string{"1", "2"},
			coinFlip:  true,
		},
		{
			desc: "Invalid",
			expected: allVotes{
				invalid: proposalSet{
					"1": struct{}{},
					"2": struct{}{},
				},
				valid: proposalSet{},
			},
			undecided: []string{"1", "2"},
			coinFlip:  false,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			votes := allVotes{valid: proposalSet{}, invalid: proposalSet{}}
			tallyUndecided(&votes, tc.undecided, tc.coinFlip)
			require.Equal(t, tc.expected, votes)
		})
	}
}
