package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_votesSetPair_Diff(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name         string
		firstRound   votesSetPair
		currentRound votesSetPair
		votesFor     proposalList
		votesAgainst proposalList
	}{
		{
			name: "Case 1",
			firstRound: votesSetPair{
				ValidVotes: hashSet{
					"0xA": {},
					"0xB": {},
				},
				InvalidVotes: hashSet{
					"0xC": {},
				},
			},
			currentRound: votesSetPair{
				ValidVotes: hashSet{
					"0xA": {},
				},
				InvalidVotes: hashSet{
					"0xB": {},
					"0xC": {},
				},
			},
			votesFor:     proposalList{},
			votesAgainst: proposalList{"0xB"},
		},
		{
			name: "Case 2",
			firstRound: votesSetPair{
				ValidVotes: hashSet{
					"0x0": {},
					"0x1": {},
					"0x2": {},
					"0x3": {},
					"0x4": {},
				},
				InvalidVotes: hashSet{
					"0x5": {},
					"0x6": {},
					"0x7": {},
					"0x8": {},
					"0x9": {},
				},
			},
			currentRound: votesSetPair{
				ValidVotes: hashSet{
					"0x0": {},
					"0x1": {},
					"0x8": {},
					"0x9": {},
				},
				InvalidVotes: hashSet{
					"0x2": {},
					"0x3": {},
					"0x4": {},
					"0x5": {},
					"0x6": {},
					"0x7": {},
				},
			},
			votesFor:     proposalList{"0x8", "0x9"},
			votesAgainst: proposalList{"0x2", "0x3", "0x4"},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			votesFor, votesAgainst := tc.currentRound.Diff(tc.firstRound)
			r.EqualValues(tc.votesFor.Sort(), votesFor.Sort())
			r.EqualValues(tc.votesAgainst.Sort(), votesAgainst.Sort())
		})
	}
}
