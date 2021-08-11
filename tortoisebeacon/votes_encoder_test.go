package tortoisebeacon

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/stretchr/testify/require"
)

func TestTortoiseBeacon_encodeVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name         string
		proposals    proposals
		firstRound   proposalsBytes
		currentRound votesSetPair
		result       []uint64
	}{
		{
			name: "Case 1",
			proposals: proposals{
				ValidProposals: []proposal{
					string(util.Hex2Bytes("11")),
					string(util.Hex2Bytes("22")),
				},
				PotentiallyValidProposals: []proposal{
					string(util.Hex2Bytes("33")),
				},
			},
			firstRound: proposalsBytes{
				ValidProposals: [][]byte{
					util.Hex2Bytes("11"),
					util.Hex2Bytes("22"),
				},
				PotentiallyValidProposals: [][]byte{
					util.Hex2Bytes("33"),
				},
			},
			currentRound: votesSetPair{
				ValidVotes: hashSet{
					string(util.Hex2Bytes("11")): {},
					string(util.Hex2Bytes("33")): {},
				},
				InvalidVotes: hashSet{
					string(util.Hex2Bytes("22")): {},
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
				Log: logtest.New(t).WithName("TortoiseBeacon"),
			}

			result := tb.encodeVotes(tc.currentRound, tc.proposals)
			r.EqualValues(tc.result, result)

			original := tb.decodeVotes(result, tc.firstRound)
			r.EqualValues(tc.currentRound, original)
		})
	}
}
