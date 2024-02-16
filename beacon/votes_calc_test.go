package beacon

import (
	"bytes"
	"math/big"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestBeacon_calcVotes(t *testing.T) {
	t.Parallel()

	const threshold = 3

	tt := []struct {
		name               string
		ownFirstRoundVotes allVotes
		votesMargin        map[Proposal]*big.Int
		result             allVotes
		undecided          proposalList
	}{
		{
			name: "Case 1",
			ownFirstRoundVotes: allVotes{
				support: proposalSet{
					Proposal{0x01}: {},
					Proposal{0x02}: {},
				},
				against: proposalSet{
					Proposal{0x03}: {},
				},
			},
			votesMargin: map[Proposal]*big.Int{
				{0x01}: big.NewInt(threshold * 2),
				{0x02}: big.NewInt(-threshold * 3),
				{0x03}: big.NewInt(threshold / 2),
			},
			result: allVotes{
				support: proposalSet{
					Proposal{0x01}: {},
				},
				against: proposalSet{
					Proposal{0x02}: {},
				},
			},
			undecided: proposalList{
				Proposal{0x03},
			},
		},
		{
			name: "Case 2",
			votesMargin: map[Proposal]*big.Int{
				{0x01}: big.NewInt(threshold * 2),
				{0x02}: big.NewInt(-threshold * 3),
				{0x03}: big.NewInt(threshold / 2),
			},
			result: allVotes{
				support: proposalSet{
					Proposal{0x01}: {},
				},
				against: proposalSet{
					Proposal{0x02}: {},
				},
			},
			undecided: proposalList{
				Proposal{0x03},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			eh := &state{
				epochWeight: uint64(threshold),
				votesMargin: tc.votesMargin,
			}
			theta := new(big.Float).SetRat(big.NewRat(1, 1))
			logger := logtest.New(t).WithName(tc.name)

			result, undecided := calcVotes(logger, theta, eh)
			sort.Slice(undecided, func(i, j int) bool { return bytes.Compare(undecided[i][:], undecided[j][:]) == -1 })
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
		undecided proposalList
		coinFlip  bool
	}{
		{
			desc: "Valid",
			expected: allVotes{
				support: proposalSet{
					Proposal{0x01}: struct{}{},
					Proposal{0x02}: struct{}{},
				},
				against: proposalSet{},
			},
			undecided: proposalList{Proposal{0x01}, Proposal{0x02}},
			coinFlip:  true,
		},
		{
			desc: "Invalid",
			expected: allVotes{
				against: proposalSet{
					Proposal{0x01}: struct{}{},
					Proposal{0x02}: struct{}{},
				},
				support: proposalSet{},
			},
			undecided: proposalList{Proposal{0x01}, Proposal{0x02}},
			coinFlip:  false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			votes := allVotes{support: proposalSet{}, against: proposalSet{}}
			tallyUndecided(&votes, tc.undecided, tc.coinFlip)
			require.Equal(t, tc.expected, votes)
		})
	}
}

func TestBeacon_votingThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name      string
		theta     *big.Rat
		weight    uint64
		threshold *big.Int
	}{
		{
			name:      "Case 1",
			theta:     big.NewRat(1, 2),
			weight:    10,
			threshold: big.NewInt(5),
		},
		{
			name:      "Case 2",
			theta:     big.NewRat(3, 10),
			weight:    10,
			threshold: big.NewInt(3),
		},
		{
			name:      "Case 3",
			theta:     big.NewRat(1, 25000),
			weight:    31744,
			threshold: big.NewInt(1),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pd := ProtocolDriver{
				logger: logtest.New(t).WithName("Beacon"),
				config: Config{},
				theta:  new(big.Float).SetRat(tc.theta),
			}

			threshold := votingThreshold(pd.theta, tc.weight)
			r.EqualValues(tc.threshold, threshold)
		})
	}
}
