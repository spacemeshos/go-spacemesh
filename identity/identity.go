package identity

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/events"
)

var ErrIdentityStateUnknown = errors.New("identity state is unknown")

type StateInfo struct {
	State State
	Time  time.Time
}

type StateStorage struct {
	db     sql.Executor
	logger *zap.Logger
}

func NewIdentityStateStorage(db sql.Executor, logger *zap.Logger) *StateStorage {
	return &StateStorage{
		db:     db,
		logger: logger,
	}
}

func (s *StateStorage) Set(id types.NodeID, newState State) {
	info := StateInfo{
		State: newState,
		Time:  time.Now(),
	}

	stateBytes, err := marshalState(&info)
	if err != nil {
		s.logger.Panic("marshaling state", zap.Error(err))
	}
	if err := events.InsertEvent(s.db, id, info.Time, stateBytes); err != nil {
		s.logger.Panic("inserting state into local DB", zap.Error(err))
	}
}

func (s *StateStorage) Get(id types.NodeID) ([]StateInfo, error) {
	var allEvents []StateInfo
	err := events.IterateEventsForID(s.db, id, func(timestamp time.Time, eventBytes []byte) bool {
		event, err := unmarshalState(eventBytes)
		if err != nil {
			s.logger.Panic(
				"unmarshaling event from DB",
				log.ZShortStringer("smesherID", id),
				zap.Time("timestamp", timestamp),
				zap.Error(err),
			)
		}
		allEvents = append(allEvents, *event)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("iterating over events for ID %s: %w", id.ShortString(), err)
	}
	if len(allEvents) == 0 {
		return nil, ErrIdentityStateUnknown
	}
	return allEvents, nil
}

func (s *StateStorage) All() map[types.NodeID][]StateInfo {
	allEvents := make(map[types.NodeID][]StateInfo)
	events.IterateAllEvents(s.db, func(id types.NodeID, timestamp time.Time, eventBytes []byte) bool {
		event, err := unmarshalState(eventBytes)
		if err != nil {
			s.logger.Panic(
				"unmarshaling event from DB",
				log.ZShortStringer("smesherID", id),
				zap.Time("timestamp", timestamp),
				zap.Error(err),
			)
		}
		if _, ok := allEvents[id]; !ok {
			allEvents[id] = make([]StateInfo, 0)
		}
		allEvents[id] = append(allEvents[id], *event)
		return true
	})
	return allEvents
}

func (s *StateStorage) SetEligibilities(
	id types.NodeID,
	eligibilities map[types.LayerID][]types.VotingEligibility,
) {
	for layer, eligibilities := range eligibilities {
		for _, eligibility := range eligibilities {
			if err := events.InsertEligibility(s.db, id, layer, &eligibility); err != nil {
				s.logger.Panic("inserting eligibility", zap.Error(err))
			}
		}
	}
}

func (s *StateStorage) AddProposal(id types.NodeID, proposal *types.Proposal) {
	if err := events.InsertProposal(s.db, proposal); err != nil {
		s.logger.Panic("failed to insert proposal", zap.Error(err))
	}
	s.Set(proposal.SmesherID, &ProposalPublished{
		Proposal: proposal.ID(),
		Layer:    proposal.Layer,
	})
}

func (s *StateStorage) AllProposals() map[types.NodeID][]*types.Proposal {
	proposals := make(map[types.NodeID][]*types.Proposal)
	events.IterateAllProposals(s.db, func(p types.Proposal) bool {
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
	events.IterateAllEligibilities(
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
