package tortoisebeacon

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/require"
)

func TestTortoiseBeacon_decodeVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name       string
		firstRound firstRoundVotes
		bitVector  []uint64
		result     votesSetPair
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
			bitVector: []uint64{0b101},
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

			result := tb.decodeVotes(tc.bitVector, tc.firstRound)
			r.EqualValues(tc.result, result)

			original := tb.encodeVotes(result, tc.firstRound)
			r.EqualValues(tc.bitVector, original)
		})
	}
}
