package activation

import (
	"context"
	"fmt"
	"maps"
	"sync"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

type postStates struct {
	log            *zap.Logger
	mu             sync.RWMutex
	states         map[types.NodeID]types.PostState
	watchingStates sync.Once
}

func newPostStates(log *zap.Logger) postStates {
	return postStates{
		log:    log,
		states: make(map[types.NodeID]types.PostState),
	}
}

func (s *postStates) set(id types.NodeID, state types.PostState) {
	s.mu.Lock()
	s.states[id] = state
	s.mu.Unlock()

	s.log.Info("post state changed", zap.Stringer("id", id), zap.Stringer("state", state))
}

func (s *postStates) get() map[types.NodeID]types.PostState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := make(map[types.NodeID]types.PostState, len(s.states))
	maps.Copy(copy, s.states)
	return copy
}

func (s *postStates) watchEvents(ctx context.Context) error {
	var result error
	s.watchingStates.Do(func() {
		events.InitializeReporter()
		sub, err := events.SubscribeMatched(func(t *events.UserEvent) bool {
			switch t.Event.Details.(type) {
			case *pb.Event_PostStart:
				return true
			case *pb.Event_PostComplete:
				return true
			default:
				return false
			}
		}, events.WithBuffer(50))
		if err != nil {
			result = fmt.Errorf("subscribing to post events: %w", err)
		}

		go func() {
			for {
				select {
				case e := <-sub.Out():
					switch e.Event.Details.(type) {
					case *pb.Event_PostStart:
						s.set(types.BytesToNodeID(e.Event.GetPostStart().Smesher), types.PostStateProving)
					case *pb.Event_PostComplete:
						s.set(types.BytesToNodeID(e.Event.GetPostComplete().Smesher), types.PostStateIdle)
					}
				case <-ctx.Done():
					sub.Close()
					return
				}
			}
		}()
	})
	return result
}
