package types

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
)

var (
	ErrInvalidIdentityStateSwitch = errors.New("invalid identity state switch")
	ErrIdentityStateUnknown       = errors.New("identity state is unknown")
)

type IdentityState int

const (
	IdentityStateWaitForATXSyncing IdentityState = iota
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
	default:
		panic(fmt.Sprintf(ErrIdentityStateUnknown.Error()+" %d", s))
	}
}

type IdentityStates interface {
	Set(id NodeID, state IdentityState) error
	Get(id NodeID) (IdentityState, error)
	GetAll() map[NodeID]IdentityState
}

type IdentityStateStorage struct {
	mu     sync.RWMutex
	states map[NodeID]IdentityState
}

func NewIdentityStateStorage() IdentityStates {
	return &IdentityStateStorage{
		states: make(map[NodeID]IdentityState),
	}
}

var validStateSwitch = map[IdentityState][]IdentityState{
	IdentityStateWaitForATXSyncing:     {IdentityStateWaitForPoetRoundStart},
	IdentityStatePostProving:           {IdentityStateWaitForPoetRoundStart},
	IdentityStateWaitForPoetRoundStart: {IdentityStateWaitForPoetRoundEnd, IdentityStateWaitForATXSyncing},
	IdentityStateWaitForPoetRoundEnd:   {IdentityStateFetchingProofs, IdentityStateWaitForPoetRoundStart},
	IdentityStateFetchingProofs:        {IdentityStatePostProving, IdentityStateWaitForPoetRoundStart},
}

func (s *IdentityStateStorage) Set(id NodeID, newState IdentityState) error {
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
	return ErrInvalidIdentityStateSwitch
}

func (s *IdentityStateStorage) Get(id NodeID) (IdentityState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists {
		return 0, ErrIdentityStateUnknown
	}
	return state, nil
}

func (s *IdentityStateStorage) GetAll() map[NodeID]IdentityState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := make(map[NodeID]IdentityState, len(s.states))
	maps.Copy(copy, s.states)
	return copy
}
