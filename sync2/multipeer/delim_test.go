package multipeer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
)

func TestGetDelimiters(t *testing.T) {
	for _, tc := range []struct {
		numPeers int
		keyLen   int
		maxDepth int
		values   []string
	}{
		{
			numPeers: 0,
			maxDepth: 64,
			keyLen:   32,
			values:   nil,
		},
		{
			numPeers: 1,
			maxDepth: 64,
			keyLen:   32,
			values:   nil,
		},
		{
			numPeers: 2,
			maxDepth: 64,
			keyLen:   32,
			values: []string{
				"8000000000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 2,
			maxDepth: 24,
			keyLen:   32,
			values: []string{
				"8000000000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 3,
			maxDepth: 64,
			keyLen:   32,
			values: []string{
				"5555555555555554000000000000000000000000000000000000000000000000",
				"aaaaaaaaaaaaaaa8000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 3,
			maxDepth: 24,
			keyLen:   32,
			values: []string{
				"5555550000000000000000000000000000000000000000000000000000000000",
				"aaaaaa0000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 3,
			maxDepth: 4,
			keyLen:   32,
			values: []string{
				"5000000000000000000000000000000000000000000000000000000000000000",
				"a000000000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 4,
			maxDepth: 64,
			keyLen:   32,
			values: []string{
				"4000000000000000000000000000000000000000000000000000000000000000",
				"8000000000000000000000000000000000000000000000000000000000000000",
				"c000000000000000000000000000000000000000000000000000000000000000",
			},
		},
		{
			numPeers: 4,
			maxDepth: 24,
			keyLen:   32,
			values: []string{
				"4000000000000000000000000000000000000000000000000000000000000000",
				"8000000000000000000000000000000000000000000000000000000000000000",
				"c000000000000000000000000000000000000000000000000000000000000000",
			},
		},
	} {
		ds := multipeer.GetDelimiters(tc.numPeers, tc.keyLen, tc.maxDepth)
		var hs []string
		for _, d := range ds {
			hs = append(hs, d.String())
		}
		if len(tc.values) == 0 {
			require.Empty(t, hs, "%d delimiters", tc.numPeers)
		} else {
			require.Equal(t, tc.values, hs, "%d delimiters", tc.numPeers)
		}
	}
}
