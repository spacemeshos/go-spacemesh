package beacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestBeacon_decodeVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name       string
		proposals  proposals
		firstRound proposals
		bitVector  []uint64
		result     allVotes
	}{
		{
			name: "Case 1",
			proposals: proposals{
				valid: [][]byte{
					util.Hex2Bytes("11"),
					util.Hex2Bytes("22"),
				},
				potentiallyValid: [][]byte{
					util.Hex2Bytes("33"),
				},
			},
			firstRound: proposals{
				valid: [][]byte{
					util.Hex2Bytes("11"),
					util.Hex2Bytes("22"),
				},
				potentiallyValid: [][]byte{
					util.Hex2Bytes("33"),
				},
			},
			bitVector: []uint64{0b101},
			result: allVotes{
				valid: proposalSet{
					string(util.Hex2Bytes("11")): {},
					string(util.Hex2Bytes("33")): {},
				},
				invalid: proposalSet{
					string(util.Hex2Bytes("22")): {},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pd := ProtocolDriver{
				config: Config{
					VotesLimit: 100,
				},
				logger: logtest.New(t).WithName("Beacon"),
			}

			result := pd.decodeVotes(tc.bitVector, tc.firstRound)
			r.EqualValues(tc.result, result)

			original := pd.encodeVotes(result, tc.proposals)
			r.EqualValues(tc.bitVector, original)
		})
	}
}
