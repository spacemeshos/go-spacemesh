package beacon

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type minerInfo struct {
	atxid     types.ATXID
	malicious bool
}

// State does the data management for epoch specific data for the protocol.
// Not thread-safe. It relies on ProtocolDriver's thread-safety mechanism.
type state struct {
	logger      *zap.Logger
	active      map[types.NodeID]participant
	epochWeight uint64
	// the original proposals as received, bucketed by validity.
	incomingProposals proposals
	// minerPublicKey -> list of proposal.
	// this list is used in encoding/decoding votes for each miner in all subsequent voting rounds.
	firstRoundIncomingVotes map[types.NodeID]proposalList
	// TODO(nkryuchkov): For every round excluding first round consider having a vector of opinions.
	votesMargin               map[Proposal]*big.Int
	hasProposed               map[types.NodeID]struct{}
	hasVoted                  map[types.NodeID]*votesTracker
	proposalPhaseFinishedTime time.Time
	proposalChecker           eligibilityChecker
	minerAtxs                 map[types.NodeID]*minerInfo
}

func newState(
	logger *zap.Logger,
	_ Config,
	active map[types.NodeID]participant,
	epochWeight uint64,
	miners map[types.NodeID]*minerInfo,
	checker eligibilityChecker,
) *state {
	return &state{
		logger:                  logger,
		epochWeight:             epochWeight,
		active:                  active,
		minerAtxs:               miners,
		firstRoundIncomingVotes: make(map[types.NodeID]proposalList),
		votesMargin:             map[Proposal]*big.Int{},
		hasProposed:             make(map[types.NodeID]struct{}),
		hasVoted:                make(map[types.NodeID]*votesTracker),
		proposalChecker:         checker,
	}
}

func (s *state) addValidProposal(proposal Proposal) {
	if s.incomingProposals.valid == nil {
		s.incomingProposals.valid = make(map[Proposal]struct{})
	}
	s.incomingProposals.valid[proposal] = struct{}{}
	s.votesMargin[proposal] = new(big.Int)
}

func (s *state) addPotentiallyValidProposal(proposal Proposal) {
	if s.incomingProposals.potentiallyValid == nil {
		s.incomingProposals.potentiallyValid = make(map[Proposal]struct{})
	}
	s.incomingProposals.potentiallyValid[proposal] = struct{}{}
	s.votesMargin[proposal] = new(big.Int)
}

func (s *state) setMinerFirstRoundVote(nodeID types.NodeID, voteList []Proposal) {
	s.firstRoundIncomingVotes[nodeID] = voteList
}

func (s *state) getMinerFirstRoundVote(nodeID types.NodeID) (proposalList, error) {
	p, ok := s.firstRoundIncomingVotes[nodeID]
	if !ok {
		return nil, errors.New("no first round votes for miner")
	}
	return p, nil
}

func (s *state) addVote(proposal Proposal, vote uint, voteWeight *big.Int) {
	if _, ok := s.votesMargin[proposal]; !ok {
		// voteMargin is updated during the proposal phase.
		// ignore votes on proposals not in the original proposals.
		s.logger.Warn("ignoring vote for unknown proposal", zap.Inline(proposal))
		return
	}
	if vote == up {
		s.votesMargin[proposal].Add(s.votesMargin[proposal], voteWeight)
	} else {
		s.votesMargin[proposal].Sub(s.votesMargin[proposal], voteWeight)
	}
}

func (s *state) registerProposed(nodeID types.NodeID) error {
	if _, ok := s.hasProposed[nodeID]; ok {
		return fmt.Errorf("already made proposal (miner ID %v): %w", nodeID.ShortString(), errAlreadyProposed)
	}

	s.hasProposed[nodeID] = struct{}{}
	return nil
}

func (s *state) registerVoted(nodeID types.NodeID, round types.RoundID) error {
	tracker, exists := s.hasVoted[nodeID]
	if !exists {
		tracker = newVotesTracker()
		s.hasVoted[nodeID] = tracker
	}
	if !tracker.register(round) {
		return fmt.Errorf("[round %v] already voted (miner ID %v): %w", round, nodeID.ShortString(), errAlreadyVoted)
	}
	return nil
}
