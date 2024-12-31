package identity

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/events"
)

var ErrIdentityStateUnknown = errors.New("identity state is unknown")

type StateInfo struct {
	State        State
	PublishEpoch *types.EpochID
	Time         time.Time
}

type StateStorage struct {
	db         sql.Executor
	mu         sync.RWMutex
	identities map[types.NodeID][]StateInfo
}

func NewIdentityStateStorage(db sql.Executor) *StateStorage {
	return &StateStorage{
		db:         db,
		identities: make(map[types.NodeID][]StateInfo),
	}
}

func NewFromDb(db sql.Executor) *StateStorage {
	s := NewIdentityStateStorage(db)
	events.IterateAllEvents(db, func(id types.NodeID, timestamp time.Time, stateBytes []byte) bool {
		state, err := unmarshalState(stateBytes)
		if err != nil {
			panic(fmt.Sprintf("unmarshaling state from DB for id %s with time=%v: %v", id, timestamp, err))
		}
		s.set(id, *state)
		return true
	})
	return s
}

func (s *StateStorage) Set(
	id types.NodeID,
	publishEpoch *types.EpochID,
	newState State,
) {
	info := StateInfo{
		State:        newState,
		PublishEpoch: publishEpoch,
		Time:         time.Now(),
	}
	s.set(id, info)

	stateBytes, err := marshalState(&info)
	if err != nil {
		panic(fmt.Sprintf("marhsaling state: %v", err))
	}
	if err := events.InsertEvent(s.db, id, info.Time, stateBytes); err != nil {
		panic(fmt.Sprintf("inserting state into local DB: %v", err))
	}
}

func (s *StateStorage) set(
	id types.NodeID,
	info StateInfo,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[id]; !exists {
		s.identities[id] = []StateInfo{}
	}

	if len(s.identities[id]) > 100 {
		s.identities[id] = s.identities[id][1:]
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

func (s *StateStorage) SetEligibilities(
	id types.NodeID,
	eligibilities map[types.LayerID][]types.VotingEligibility,
) {
	for layer, eligibilities := range eligibilities {
		for _, eligibility := range eligibilities {
			if err := events.InsertEligibility(s.db, id, layer, &eligibility); err != nil {
				panic(fmt.Sprintf("inserting eligibility: %v", err))
			}
		}
	}
}

func (s *StateStorage) AddProposal(id types.NodeID, proposal *types.Proposal) {
	if err := events.InsertProposal(s.db, proposal); err != nil {
		panic(fmt.Sprintf("failed to insert proposal: %v", err))
	}
	s.Set(proposal.SmesherID, nil, &ProposalPublished{
		Proposal: proposal.ID(),
		Layer:    proposal.Layer,
	})
}

func (s *StateStorage) AllProposals() map[types.NodeID][]*types.Proposal {
	proposals := make(map[types.NodeID][]*types.Proposal)
	events.InterateAllProposals(s.db, func(p types.Proposal) bool {
		if _, ok := proposals[p.SmesherID]; !ok {
			proposals[p.SmesherID] = make([]*types.Proposal, 0)
		}
		proposals[p.SmesherID] = append(proposals[p.SmesherID], &p)
		return true
	})
	return proposals
}

func (s *StateStorage) AllEligibilities() map[types.NodeID]map[types.LayerID][]types.VotingEligibility {
	eligibilities := make(map[types.NodeID]map[types.LayerID][]types.VotingEligibility)
	events.InterateAllEligibilities(
		s.db,
		func(id types.NodeID, layer types.LayerID, eligibility *types.VotingEligibility) bool {
			if _, ok := eligibilities[id]; !ok {
				eligibilities[id] = make(map[types.LayerID][]types.VotingEligibility)
			}
			if _, ok := eligibilities[id][layer]; !ok {
				eligibilities[id][layer] = make([]types.VotingEligibility, 0)
			}
			eligibilities[id][layer] = append(eligibilities[id][layer], *eligibility)
			return true
		},
	)
	return eligibilities
}
