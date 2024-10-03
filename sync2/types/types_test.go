package types

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirst(t *testing.T) {
	seq := Seq(slices.Values([]KeyBytes{{1}, {2}, {3}, {4}}))
	require.Equal(t, KeyBytes{1}, seq.First())
}

func TestGetN(t *testing.T) {
	seq := Seq(slices.Values([]KeyBytes{{1}, {2}, {3}, {4}}))
	require.Empty(t, seq.GetN(0))
	require.Equal(t, []KeyBytes{{1}}, seq.GetN(1))
	require.Equal(t, []KeyBytes{{1}, {2}}, seq.GetN(2))
	require.Equal(t, []KeyBytes{{1}, {2}, {3}}, seq.GetN(3))
	require.Equal(t, []KeyBytes{{1}, {2}, {3}, {4}}, seq.GetN(4))
	require.Equal(t, []KeyBytes{{1}, {2}, {3}, {4}}, seq.GetN(5))
}

func TestIncID(t *testing.T) {
	for _, tc := range []struct {
		id, expected KeyBytes
		overflow     bool
	}{
		{
			id:       KeyBytes{0x00, 0x00, 0x00, 0x00},
			expected: KeyBytes{0x00, 0x00, 0x00, 0x01},
			overflow: false,
		},
		{
			id:       KeyBytes{0x00, 0x00, 0x00, 0xff},
			expected: KeyBytes{0x00, 0x00, 0x01, 0x00},
			overflow: false,
		},
		{
			id:       KeyBytes{0xff, 0xff, 0xff, 0xff},
			expected: KeyBytes{0x00, 0x00, 0x00, 0x00},
			overflow: true,
		},
	} {
		id := make(KeyBytes, len(tc.id))
		copy(id, tc.id)
		require.Equal(t, tc.overflow, id.Inc())
		require.Equal(t, tc.expected, id)
	}
}
