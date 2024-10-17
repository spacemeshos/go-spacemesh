package activation

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	ErrIdentityStateUnknown       = errors.New("identity state is unknown")
	ErrInvalidIdentityStateSwitch = errors.New("invalid identity state switch")
)

type IdentityState int

const (
	IdentityStateNotSet IdentityState = iota
	IdentityStateWaitForATXSyncing
	IdentityStateWaitForPoetRoundStart
	IdentityStateWaitForPoetRoundEnd
	IdentityStateFetchingProofs
	IdentityStatePostProving
)

func (s IdentityState) String() string {
	switch s {
	case IdentityStateWaitForATXSyncing:
		return "wait_for_atx_syncing"
	case IdentityStateWaitForPoetRoundStart:
		return "wait_for_poet_round_start"
	case IdentityStateWaitForPoetRoundEnd:
		return "wait_for_poet_round_end"
	case IdentityStateFetchingProofs:
		return "fetching_proofs"
	case IdentityStatePostProving:
		return "post_proving"
	case IdentityStateNotSet:
		return "not_set"
	default:
		panic(fmt.Sprintf(ErrIdentityStateUnknown.Error()+" %d", s))
	}
}

type IdentityStateStorage struct {
	mu     sync.RWMutex
	states map[types.NodeID]IdentityState
}

func NewIdentityStateStorage() *IdentityStateStorage {
	return &IdentityStateStorage{
		states: make(map[types.NodeID]IdentityState),
	}
}

var validStateSwitch = map[IdentityState][]IdentityState{
	IdentityStateWaitForATXSyncing: {
		IdentityStateWaitForPoetRoundStart,
	},
	IdentityStatePostProving: {
		IdentityStateWaitForPoetRoundStart,
	},
	IdentityStateWaitForPoetRoundStart: {
		IdentityStateWaitForPoetRoundEnd,
		IdentityStateWaitForATXSyncing,
	},
	IdentityStateWaitForPoetRoundEnd: {
		IdentityStateFetchingProofs,
		IdentityStateWaitForPoetRoundStart,
	},
	IdentityStateFetchingProofs: {
		IdentityStatePostProving,
		IdentityStateWaitForPoetRoundStart,
	},
}

func (s *IdentityStateStorage) Set(id types.NodeID, newState IdentityState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentState, exists := s.states[id]
	switch {
	case !exists:
		if newState == IdentityStateWaitForATXSyncing {
			s.states[id] = newState
			return nil
		}
	case currentState == newState:
		return nil

	default:
		if validNextStates, ok := validStateSwitch[currentState]; ok &&
			slices.Contains(validNextStates, newState) {
			s.states[id] = newState
			return nil
		}
	}

	return fmt.Errorf(
		"%w: state %v can't be switched to %v",
		ErrInvalidIdentityStateSwitch,
		currentState,
		newState,
	)
}

func (s *IdentityStateStorage) Get(id types.NodeID) (IdentityState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists {
		return 0, ErrIdentityStateUnknown
	}
	return state, nil
}

func (s *IdentityStateStorage) All() map[types.NodeID]IdentityState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.states)
}
