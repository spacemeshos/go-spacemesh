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

	IdentityStateWaitForATXSyncing

	// poet
	IdentityStateWaitingForPoetRegistrationWindow
	// building nipost challenge
	IdentityStatePoetChallengeReady
	IdentityStatePoetRegistered
	IdentityStatePoetRegistrationFailed
	// 2w pass ...
	IdentityStateWaitForPoetRoundEnd
	IdentityStatePoetProofReceived
	IdentityStatePoetProofFailed

	// post
	IdentityStateGeneratingPostProof
	IdentityStatePostProofReady
	IdentityStatePostProofFailed

	// atx
	IdentityStateATXExpired
	IdentityStateATXReady
	IdentityStateATXBroadcasted
)

func (s IdentityState) String() string {
	switch s {
	case IdentityStateNotSet:
		return "not set"
	case IdentityStateWaitForATXSyncing:
		return "wait for atx syncing"
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
	case IdentityStatePoetProofFailed:
		return "poet proof failed"
	case IdentityStateGeneratingPostProof:
		return "generating post proof"
	case IdentityStatePostProofReady:
		return "post proof ready"
	case IdentityStatePostProofFailed:
		return "post proof failed"
	case IdentityStateATXReady:
		return "atx ready"
	case IdentityStateATXBroadcasted:
		return "atx broadcasted"
	default:
		panic(fmt.Sprintf(ErrIdentityStateUnknown.Error()+" %d", s))
	}
}

type IdentityStateInfo struct {
	Message string
	Time    time.Time
}

type IdentityInfo struct {
	PublishEpoch types.EpochID
	States       map[IdentityState]IdentityStateInfo
}

type Identity struct {
	EpochStates map[types.EpochID]*IdentityInfo
	States      map[IdentityState]IdentityStateInfo
}

type IdentityStateStorage struct {
	mu         sync.RWMutex
	identities map[types.NodeID]*Identity
}

func NewIdentityStateStorage() *IdentityStateStorage {
	return &IdentityStateStorage{
		identities: make(map[types.NodeID]*Identity),
	}
}

// TODO: validate state switch
//var validStateSwitch = map[IdentityState][]IdentityState{
//	IdentityStateWaitForATXSyncing: {
//		IdentityStateWaitForPoetRoundStart,
//	},
//	IdentityStatePostProving: {
//		IdentityStateWaitForPoetRoundStart,
//	},
//	IdentityStateWaitForPoetRoundStart: {
//		IdentityStateWaitForPoetRoundEnd,
//		IdentityStateWaitForATXSyncing,
//	},
//	IdentityStateWaitForPoetRoundEnd: {
//		IdentityStateFetchingProofs,
//		IdentityStateWaitForPoetRoundStart,
//	},
//	IdentityStateFetchingProofs: {
//		IdentityStatePostProving,
//		IdentityStateWaitForPoetRoundStart,
//	},
//}

func (s *IdentityStateStorage) Set(id types.NodeID, publishEpoch *types.EpochID, newState IdentityState, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[id]; !exists {
		s.identities[id] = &Identity{
			EpochStates: map[types.EpochID]*IdentityInfo{},
			States:      map[IdentityState]IdentityStateInfo{},
		}
	}

	if publishEpoch != nil {
		if _, exists := s.identities[id].EpochStates[*publishEpoch]; !exists {
			s.identities[id].EpochStates[*publishEpoch] = &IdentityInfo{
				PublishEpoch: *publishEpoch,
				States:       make(map[IdentityState]IdentityStateInfo),
			}
		}
		s.identities[id].EpochStates[*publishEpoch].States[newState] = IdentityStateInfo{
			Time:    time.Now(),
			Message: message,
		}
	} else {
		s.identities[id].States[newState] = IdentityStateInfo{
			Time:    time.Now(),
			Message: message,
		}
	}
	// TODO: validate state switch
	//currentState, exists := s.states[id]
	//switch {
	//case !exists:
	//	if newState == IdentityStateWaitForATXSyncing {
	//		s.states[id] = newState
	//		return nil
	//	}
	//case currentState == newState:
	//	return nil
	//
	//default:
	//	if validNextStates, ok := validStateSwitch[currentState]; ok &&
	//		slices.Contains(validNextStates, newState) {
	//		s.states[id] = newState
	//		return nil
	//	}
	//}
	//
	//return fmt.Errorf(
	//	"%w: state %v can't be switched to %v",
	//	ErrInvalidIdentityStateSwitch,
	//	currentState,
	//	newState,
	//)
}

func (s *IdentityStateStorage) Get(id types.NodeID) (*Identity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.identities[id]
	if !exists {
		return nil, ErrIdentityStateUnknown
	}
	return state, nil
}

func (s *IdentityStateStorage) All() map[types.NodeID]*Identity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identities
}
