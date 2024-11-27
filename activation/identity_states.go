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

	IdentityStateProposalPublished
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
	case IdentityStateProposalPublished:
		return "proposal published"
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
	mu            sync.RWMutex
	identities    map[types.NodeID][]IdentityStateInfo
	eligibilities map[types.NodeID]map[types.EpochID]map[types.LayerID][]types.VotingEligibility
	proposals     map[types.NodeID][]*types.Proposal
}

func NewIdentityStateStorage() *IdentityStateStorage {
	return &IdentityStateStorage{
		identities:    make(map[types.NodeID][]IdentityStateInfo),
		eligibilities: make(map[types.NodeID]map[types.EpochID]map[types.LayerID][]types.VotingEligibility),
		proposals:     make(map[types.NodeID][]*types.Proposal),
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

func (s *IdentityStateStorage) SetEligibilitiesForEpoch(
	id types.NodeID,
	epoch types.EpochID,
	eligibilities map[types.LayerID][]types.VotingEligibility,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.eligibilities[id]; !exists {
		s.eligibilities[id] = make(map[types.EpochID]map[types.LayerID][]types.VotingEligibility)
	}

	if len(s.eligibilities[id]) > 100 {
		delete(s.eligibilities[id], epoch-100)
	}

	if _, exists := s.eligibilities[id][epoch]; !exists {
		s.eligibilities[id][epoch] = make(map[types.LayerID][]types.VotingEligibility)
	}

	s.eligibilities[id][epoch] = eligibilities
}

func (s *IdentityStateStorage) AddProposal(id types.NodeID, proposal *types.Proposal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[id]; !exists {
		s.proposals[id] = []*types.Proposal{}
	}

	if len(s.identities[id]) > 100 {
		s.proposals[id] = s.proposals[id][1:]
	}

	s.proposals[id] = append(s.proposals[id], proposal)
	s.identities[id] = append(s.identities[id], IdentityStateInfo{
		State:   IdentityStateProposalPublished,
		Message: fmt.Sprintf("proposal %s published at layer %d", proposal.ID(), proposal.Layer.Uint32()),
		Time:    time.Now(),
	})
}

func (s *IdentityStateStorage) AllProposals() map[types.NodeID][]*types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proposals
}

//nolint:lll
func (s *IdentityStateStorage) AllEligibilities() map[types.NodeID]map[types.EpochID]map[types.LayerID][]types.VotingEligibility {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eligibilities
}
