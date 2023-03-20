package beacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_proposalList_sort(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name     string
		hashes   proposalList
		expected proposalList
	}{
		{
			name:     "Sorted order remains not changed",
			hashes:   proposalList{[4]byte{0x01}, [4]byte{0x02}, [4]byte{0x03}},
			expected: proposalList{[4]byte{0x01}, [4]byte{0x02}, [4]byte{0x03}},
		},
		{
			name:     "Unsorted order gets sorted",
			hashes:   proposalList{[4]byte{0x02}, [4]byte{0x05}, [4]byte{0x03}, [4]byte{0x01}, [4]byte{0x04}},
			expected: proposalList{[4]byte{0x01}, [4]byte{0x02}, [4]byte{0x03}, [4]byte{0x04}, [4]byte{0x05}},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.hashes.sort()
			r.EqualValues(tc.expected, got)
		})
	}
}

func Test_proposalList_hash(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name     string
		hashes   proposalList
		expected types.Hash32
	}{
		{
			name:     "Case 1",
			hashes:   proposalList{[4]byte{0x01}, [4]byte{0x02}, [4]byte{0x03}},
			expected: types.HexToHash32("0x8f2bf1c17336bff6a6ceebfae7a289a77ce447e4e4e0b6f1cf4a19cdd3de2749"),
		},
		{
			name: "Case 2",
			hashes: proposalList{
				[4]byte{0x01},
				[4]byte{0x02},
				[4]byte{0x04},
				[4]byte{0x05},
			},
			expected: types.HexToHash32("0x98f88210b68dd555d34b942072e5d3257bc0abeb6481c67fb96ffe78f479a3aa"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.hashes.hash()
			r.EqualValues(tc.expected.String(), got.String())
		})
	}
}
