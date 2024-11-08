package activation

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestStateSwitch(t *testing.T) {
	t.Run("invalid initial state", func(t *testing.T) {
		storage := NewIdentityStateStorage()
		nodeId := types.RandomNodeID()

		for state := range validStateSwitch {
			if state != IdentityStateWaitForATXSyncing && state != IdentityStateWaitForPoetRoundStart {
				require.ErrorIs(t, storage.Set(nodeId, state), ErrInvalidIdentityStateSwitch)
			}
		}
	})

	t.Run("verify all possible switches", func(t *testing.T) {
		storage := NewIdentityStateStorage()
		nodeId := types.RandomNodeID()

		require.NoError(t, storage.Set(nodeId, IdentityStateWaitForATXSyncing))

		curState, err := storage.Get(nodeId)
		require.NoError(t, err)
		require.Equal(t, IdentityStateWaitForATXSyncing, curState)

		metStates := make(map[IdentityState]struct{})
		metStates[IdentityStateWaitForATXSyncing] = struct{}{}

		for len(metStates) != len(validStateSwitch) {
			for _, possibleStates := range validStateSwitch {
				for _, state := range possibleStates {
					// try switch to all existing states except of valid
					validSwitches := validStateSwitch[curState]

					if !slices.Contains(validSwitches, state) && state != curState {
						require.ErrorIs(t, storage.Set(nodeId, state), ErrInvalidIdentityStateSwitch)
					}
				}
			}

			// switch to one of valid states, which didn't visit before
			validStates := validStateSwitch[curState]
			for i, newState := range validStates {
				if _, ok := metStates[newState]; ok {
					if i == len(validStates)-1 {
						// met all states
						return
					}
					continue
				}

				require.NoError(t, storage.Set(nodeId, newState))

				curState, err = storage.Get(nodeId)
				require.NoError(t, err)
				require.Equal(t, curState, newState)

				metStates[curState] = struct{}{}
				break
			}
		}
	})
}
