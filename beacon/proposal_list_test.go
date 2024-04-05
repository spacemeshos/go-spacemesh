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
			expected: types.HexToHash32("0xdf07f4904a7ccb203dc935dae7baf4a4c1f0ab9279a863a69109be74f26032ef"),
		},
		{
			name: "Case 2",
			hashes: proposalList{
				[4]byte{0x01},
				[4]byte{0x02},
				[4]byte{0x04},
				[4]byte{0x05},
			},
			expected: types.HexToHash32("0xe69fd154a11dc1cc3ec2036780c9b9ac36e5c4d781ab5972141a84f9acaaa4b5"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.hashes.hash()
			r.EqualValues(tc.expected.String(), got.String())
		})
	}
}
