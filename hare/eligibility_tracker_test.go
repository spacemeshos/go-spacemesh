package hare_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestEligibilityTracker_Track(t *testing.T) {
	et := hare.NewEligibilityTracker(3)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	round := uint32(3)
	require.False(t, et.Dishonest(sig.NodeID(), round))
	require.False(t, et.Track(sig.NodeID(), round, 1, true))
	require.False(t, et.Dishonest(sig.NodeID(), round))
	require.False(t, et.Track(sig.NodeID(), round, 1, false))
	require.True(t, et.Dishonest(sig.NodeID(), round))
	require.True(t, et.Track(sig.NodeID(), round, 1, true))
	require.True(t, et.Dishonest(sig.NodeID(), round))
}

func TestEligibilityTracker(t *testing.T) {
	const (
		totalNodes        = 5
		count      uint16 = 2
	)
	et := hare.NewEligibilityTracker(totalNodes)
	rounds := []uint32{0, 1, 2, 3, 4}
	nodeIDs := map[types.NodeID]bool{}
	for i := 0; i < totalNodes; i++ {
		honest := false
		if i%2 == 0 {
			honest = true
		}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		nodeIDs[sig.NodeID()] = honest
		for _, r := range rounds {
			require.False(t, et.Track(sig.NodeID(), r, count, honest))
			require.Equal(t, !honest, et.Dishonest(sig.NodeID(), r))
		}
	}
	for _, r := range rounds {
		total := 0
		good := 0
		et.ForEach(r, func(k types.NodeID, cred *hare.Cred) {
			total++
			honest, ok := nodeIDs[k]
			require.True(t, ok)
			require.Equal(t, honest, cred.Honest)
			require.Equal(t, count, cred.Count)
			if cred.Honest {
				good++
			}
		})
		require.Equal(t, totalNodes, total)
		require.Equal(t, 3, good)
	}

	// update everyone to be honest have no effect
	for key := range nodeIDs {
		for _, r := range rounds {
			dishonest := et.Dishonest(key, r)
			require.Equal(t, dishonest, et.Track(key, r, count, true))
			require.Equal(t, dishonest, et.Dishonest(key, r))
		}
	}
	for _, r := range rounds {
		total := 0
		good := 0
		et.ForEach(r, func(k types.NodeID, cred *hare.Cred) {
			total++
			honest, ok := nodeIDs[k]
			require.True(t, ok)
			require.Equal(t, honest, cred.Honest)
			require.Equal(t, count, cred.Count)
			if cred.Honest {
				good++
			}
		})
		require.Equal(t, totalNodes, total)
		require.Equal(t, 3, good)
	}

	// update everyone to be dishonest will update the tracker
	for key := range nodeIDs {
		for _, r := range rounds {
			et.Track(key, r, count, false)
		}
	}
	for _, r := range rounds {
		total := 0
		good := 0
		et.ForEach(r, func(k types.NodeID, cred *hare.Cred) {
			total++
			_, ok := nodeIDs[k]
			require.True(t, ok)
			require.Equal(t, false, cred.Honest)
			require.Equal(t, count, cred.Count)
			if cred.Honest {
				good++
			}
		})
		require.Equal(t, totalNodes, total)
		require.Equal(t, 0, good)
	}
}
