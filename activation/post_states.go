package activation

import (
	"maps"
	"sync"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type postStates struct {
	log    *zap.Logger
	mu     sync.RWMutex
	states map[types.NodeID]types.PostState
}

func NewPostStates(log *zap.Logger) *postStates {
	return &postStates{
		log:    log,
		states: make(map[types.NodeID]types.PostState),
	}
}

func (s *postStates) Set(id types.NodeID, state types.PostState) {
	s.mu.Lock()
	s.states[id] = state
	s.mu.Unlock()

	s.log.Info("post state changed", zap.Stringer("id", id), zap.Stringer("state", state))
}

func (s *postStates) Get() map[types.NodeID]types.PostState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := make(map[types.NodeID]types.PostState, len(s.states))
	maps.Copy(copy, s.states)
	return copy
}
