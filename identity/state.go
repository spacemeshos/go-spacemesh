package identity

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var ErrIdentityStateUnknown = errors.New("identity state is unknown")

type State int

const (
	StateNotSet State = iota

	StateWaitForATXSynced
	StateRetrying

	// poet.
	StateWaitingForPoetRegistrationWindow
	// building nipost challenge.
	StatePoetChallengeReady
	StatePoetRegistered
	// 2w pass ...
	StateWaitForPoetRoundEnd
	StatePoetProofReceived

	// post.
	StateGeneratingPostProof
	StatePostProofReady

	// atx.
	StateATXReady
	StateATXBroadcasted

	StateProposalPublished
)

func (s State) String() string {
	switch s {
	case StateNotSet:
		return "not set"
	case StateWaitForATXSynced:
		return "wait for atx synced"
	case StateRetrying:
		return "retrying"
	case StatePoetChallengeReady:
		return "poet challenge ready"
	case StateWaitingForPoetRegistrationWindow:
		return "waiting for poet registration window"
	case StatePoetRegistered:
		return "poet registered"
	case StateWaitForPoetRoundEnd:
		return "wait for poet round end"
	case StatePoetProofReceived:
		return "poet proof received"
	case StateGeneratingPostProof:
		return "generating post proof"
	case StatePostProofReady:
		return "post proof ready"
	case StateATXReady:
		return "atx ready"
	case StateATXBroadcasted:
		return "atx broadcasted"
	case StateProposalPublished:
		return "proposal published"
	default:
		panic(fmt.Sprintf(ErrIdentityStateUnknown.Error()+" %d", s))
	}
}

type (
	StateInfoMetadata func(*StateInfo)
	StateInfo         struct {
		State        State
		PublishEpoch *types.EpochID
		Time         time.Time

		RetryingState            *RetryingStateMetadata
		PoetRegisteredState      *PoetRegisteredStateMetadata
		WaitForPoetRoundEndState *WaitForPoetRoundEndStateMetadata
		PoetProofReceivedState   *PoetProofReceivedStateMetadata
		AtxBroadcastedState      *AtxBroadcastedStateMetadata
		ProposalPublishedState   *ProposalPublishedStateMetadata
	}
)

type StateStorage struct {
	mu            sync.RWMutex
	identities    map[types.NodeID][]StateInfo
	eligibilities map[types.NodeID]map[types.EpochID]map[types.LayerID][]types.VotingEligibility
	proposals     map[types.NodeID][]*types.Proposal
}

func NewIdentityStateStorage() *StateStorage {
	return &StateStorage{
		identities:    make(map[types.NodeID][]StateInfo),
		eligibilities: make(map[types.NodeID]map[types.EpochID]map[types.LayerID][]types.VotingEligibility),
		proposals:     make(map[types.NodeID][]*types.Proposal),
	}
}

func (s *StateStorage) Set(
	id types.NodeID,
	publishEpoch *types.EpochID,
	newState State,
	metadata ...StateInfoMetadata,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[id]; !exists {
		s.identities[id] = []StateInfo{}
	}

	if len(s.identities[id]) > 100 {
		s.identities[id] = s.identities[id][1:]
	}

	info := StateInfo{
		State:        newState,
		PublishEpoch: publishEpoch,
		Time:         time.Now(),
	}

	for _, data := range metadata {
		data(&info)
	}

	s.identities[id] = append(s.identities[id], info)
}

func (s *StateStorage) Get(id types.NodeID) ([]StateInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.identities[id]
	if !exists {
		return nil, ErrIdentityStateUnknown
	}
	return state, nil
}

func (s *StateStorage) All() map[types.NodeID][]StateInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identities
}

func (s *StateStorage) SetEligibilitiesForEpoch(
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

func (s *StateStorage) AddProposal(id types.NodeID, proposal *types.Proposal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[id]; !exists {
		s.proposals[id] = []*types.Proposal{}
	}

	if len(s.identities[id]) > 100 {
		s.proposals[id] = s.proposals[id][1:]
	}

	s.proposals[id] = append(s.proposals[id], proposal)
	s.identities[id] = append(s.identities[id], StateInfo{
		State: StateProposalPublished,
		ProposalPublishedState: &ProposalPublishedStateMetadata{
			Proposal: proposal.ID(),
			Layer:    proposal.Layer,
		},
		Time: time.Now(),
	})
}

func (s *StateStorage) AllProposals() map[types.NodeID][]*types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proposals
}

//nolint:lll
func (s *StateStorage) AllEligibilities() map[types.NodeID]map[types.EpochID]map[types.LayerID][]types.VotingEligibility {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eligibilities
}
