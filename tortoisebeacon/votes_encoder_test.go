package tortoisebeacon

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/require"
)

func TestTortoiseBeacon_encodeVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name         string
		firstRound   firstRoundVotes
		currentRound votesSetPair
		result       []uint64
	}{
		{
			name: "Case 1",
			firstRound: firstRoundVotes{
				ValidVotes: []proposal{
					"0x1",
					"0x2",
				},
				PotentiallyValidVotes: []proposal{
					"0x3",
				},
			},
			currentRound: votesSetPair{
				ValidVotes: hashSet{
					"0x1": {},
					"0x3": {},
				},
				InvalidVotes: hashSet{
					"0x2": {},
				},
			},

			result: []uint64{0b101},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					VotesLimit: 100,
				},
				Log: log.NewDefault("TortoiseBeacon"),
			}

			result := tb.encodeVotes(tc.currentRound, tc.firstRound)
			r.EqualValues(tc.result, result)

			original := tb.decodeVotes(result, tc.firstRound)
			r.EqualValues(tc.currentRound, original)
		})
	}
}
