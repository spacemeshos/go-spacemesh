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
	WaitForATXSyncing IdentityState = iota
	WaitForPoetRoundStart
	WaitForPoetRoundEnd
	FetchingProofs
	PostProving
)

func (s IdentityState) String() string {
	switch s {
	case WaitForATXSyncing:
		return "wait_for_atx_syncing"
	case WaitForPoetRoundStart:
		return "wait_for_poet_round_start"
	case WaitForPoetRoundEnd:
		return "wait_for_poet_round_end"
	case FetchingProofs:
		return "fetching_proofs"
	case PostProving:
		return "post_proving"
	default:
		panic(fmt.Sprintf("unknown post state %d", s))
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
	WaitForATXSyncing:     {WaitForPoetRoundStart},
	PostProving:           {WaitForPoetRoundStart},
	WaitForPoetRoundStart: {WaitForPoetRoundEnd, WaitForATXSyncing},
	WaitForPoetRoundEnd:   {FetchingProofs, WaitForPoetRoundStart},
	FetchingProofs:        {PostProving, WaitForPoetRoundStart},
}

func (s *IdentityStateStorage) Set(id NodeID, newState IdentityState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentState, exists := s.states[id]
	if !exists {
		if newState == WaitForATXSyncing {
			s.states[id] = newState
			return nil
		}
	}

	if currentState == newState {
		return nil
	}

	if validNextStates, ok := validStateSwitch[currentState]; ok && slices.Contains(validNextStates, newState) {
		s.states[id] = newState
		return nil
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
