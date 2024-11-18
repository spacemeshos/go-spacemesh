package activation

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	ErrIdentityStateUnknown       = errors.New("identity state is unknown")
	ErrInvalidIdentityStateSwitch = errors.New("invalid identity state switch")
)

type IdentityState int

const (
	IdentityStateNotSet IdentityState = iota

	IdentityStateWaitForATXSynced
	IdentityStateRetrying

	// poet.
	IdentityStateWaitingForPoetRegistrationWindow
	// building nipost challenge.
	IdentityStatePoetChallengeReady
	IdentityStatePoetRegistered
	// 2w pass ...
	IdentityStateWaitForPoetRoundEnd
	IdentityStatePoetProofReceived

	// post.
	IdentityStateGeneratingPostProof
	IdentityStatePostProofReady

	// atx.
	IdentityStateATXReady
	IdentityStateATXBroadcasted
)

func (s IdentityState) String() string {
	switch s {
	case IdentityStateNotSet:
		return "not set"
	case IdentityStateWaitForATXSynced:
		return "wait for atx synced"
	case IdentityStateRetrying:
		return "retrying"
	case IdentityStatePoetChallengeReady:
		return "poet challenge ready"
	case IdentityStateWaitingForPoetRegistrationWindow:
		return "waiting for poet registration window"
	case IdentityStatePoetRegistered:
		return "poet registered"
	case IdentityStateWaitForPoetRoundEnd:
		return "wait for poet round end"
	case IdentityStatePoetProofReceived:
		return "poet proof received"
	case IdentityStateGeneratingPostProof:
		return "generating post proof"
	case IdentityStatePostProofReady:
		return "post proof ready"
	case IdentityStateATXReady:
		return "atx ready"
	case IdentityStateATXBroadcasted:
		return "atx broadcasted"
	default:
		panic(fmt.Sprintf(ErrIdentityStateUnknown.Error()+" %d", s))
	}
}

type IdentityStateInfo struct {
	State        IdentityState
	PublishEpoch *types.EpochID
	Message      string
	Time         time.Time
}

type IdentityStateStorage struct {
	mu         sync.RWMutex
	identities map[types.NodeID][]IdentityStateInfo
}

func NewIdentityStateStorage() *IdentityStateStorage {
	return &IdentityStateStorage{
		identities: make(map[types.NodeID][]IdentityStateInfo),
	}
}

func (s *IdentityStateStorage) Set(
	id types.NodeID,
	publishEpoch *types.EpochID,
	newState IdentityState,
	message string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[id]; !exists {
		s.identities[id] = []IdentityStateInfo{}
	}

	if len(s.identities[id]) > 100 {
		s.identities[id] = s.identities[id][1:]
	}

	s.identities[id] = append(s.identities[id], IdentityStateInfo{
		State:        newState,
		PublishEpoch: publishEpoch,
		Message:      message,
		Time:         time.Now(),
	})
}

func (s *IdentityStateStorage) Get(id types.NodeID) ([]IdentityStateInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.identities[id]
	if !exists {
		return nil, ErrIdentityStateUnknown
	}
	return state, nil
}

func (s *IdentityStateStorage) All() map[types.NodeID][]IdentityStateInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identities
}
