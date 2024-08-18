package types

import (
	"github.com/stretchr/testify/require"
	"slices"
	"testing"
)

func TestStateSwitch(t *testing.T) {
	t.Run("invalid initial state", func(t *testing.T) {
		storage := NewIdentityStateStorage()
		nodeId := RandomNodeID()

		for state := range validStateSwitch {
			if state != WaitForATXSyncing && state != WaitForPoetRoundStart {
				require.ErrorIs(t, storage.Set(nodeId, state), ErrInvalidIdentityStateSwitch)
			}
		}
	})

	t.Run("verify all possible switches", func(t *testing.T) {
		storage := NewIdentityStateStorage()
		nodeId := RandomNodeID()

		require.NoError(t, storage.Set(nodeId, WaitForATXSyncing))

		curState, err := storage.Get(nodeId)
		require.NoError(t, err)
		require.Equal(t, curState, WaitForATXSyncing)

		metStates := make(map[IdentityState]struct{})
		metStates[WaitForATXSyncing] = struct{}{}

		for len(metStates) != len(validStateSwitch) {
			// check all invalid states for given current state
			for _, newStates := range validStateSwitch {
				for _, state := range newStates {
					validSwitches := validStateSwitch[curState]

					if !slices.Contains(validSwitches, state) && state != curState {
						require.ErrorIs(t, storage.Set(nodeId, state), ErrInvalidIdentityStateSwitch)
					}
				}
			}

			// switch to one of valid states, which didn't meet before
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
