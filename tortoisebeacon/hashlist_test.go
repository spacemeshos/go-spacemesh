package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_hashList_Hash(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name     string
		hashes   hashList
		expected types.Hash32
	}{
		{
			name:     "Case 1",
			hashes:   hashList{types.HexToHash32("0x1"), types.HexToHash32("0x2"), types.HexToHash32("0x3")},
			expected: types.HexToHash32("0x9701f34c80e1ef7f8125e5d4d2d7e19b509e25d26e462d5308b5abb95b64783e"),
		},
		{
			name: "Case 2",
			hashes: hashList{
				types.HexToHash32("0x1"),
				types.HexToHash32("0x2"),
				types.HexToHash32("0x4"),
				types.HexToHash32("0x5"),
			},
			expected: types.HexToHash32("0xd04dd0faf9b5d3baf04dd99152971b5db67b0b3c79e5cc59f8f7b03ab20673f8"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.hashes.Hash()
			r.EqualValues(tc.expected.String(), got.String())
		})
	}
}
