package tortoisebeacon

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
			hashes:   proposalList{"0x1", "0x2", "0x3"},
			expected: proposalList{"0x1", "0x2", "0x3"},
		},
		{
			name:     "Unsorted order gets sorted",
			hashes:   proposalList{"0x2", "0x5", "0x3", "0x1", "0x4"},
			expected: proposalList{"0x1", "0x2", "0x3", "0x4", "0x5"},
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
			hashes:   proposalList{"0x1", "0x2", "0x3"},
			expected: types.HexToHash32("0x4483077453c48a69fa6c4d9ca8e75b5fd01375f6aa8e6c7b2ccded97b8d81ae3"),
		},
		{
			name: "Case 2",
			hashes: proposalList{
				"0x1",
				"0x2",
				"0x4",
				"0x5",
			},
			expected: types.HexToHash32("0x6d148de54cc5ac334cdf4537018209b0e9f5ea94c049417103065eac777ddb5c"),
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
