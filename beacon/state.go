package beacon

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// state does the data management for epoch specific data for the protocol.
// not thread-safe. it relies on ProtocolDriver's thread-safety mechanism.
type state struct {
	cfg         Config
	logger      log.Log
	nonce       *types.VRFPostIndex
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
	minerAtxs                 map[types.NodeID]types.ATXID
}

func newState(
	logger log.Log,
	cfg Config,
	nonce *types.VRFPostIndex,
	epochWeight uint64,
	miners map[types.NodeID]types.ATXID,
	checker eligibilityChecker,
) *state {
	return &state{
		cfg:                     cfg,
		logger:                  logger,
		epochWeight:             epochWeight,
		nonce:                   nonce,
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
		return nil, fmt.Errorf("no first round votes for miner")
	}
	return p, nil
}

func (s *state) addVote(proposal Proposal, vote uint, voteWeight *big.Int) {
	if _, ok := s.votesMargin[proposal]; !ok {
		// voteMargin is updated during the proposal phase.
		// ignore votes on proposals not in the original proposals.
		s.logger.With().Warning("ignoring vote for unknown proposal",
			log.String("proposal", hex.EncodeToString(proposal[:])),
		)
		return
	}
	if vote == up {
		s.votesMargin[proposal].Add(s.votesMargin[proposal], voteWeight)
	} else {
		s.votesMargin[proposal].Sub(s.votesMargin[proposal], voteWeight)
	}
}

func (s *state) registerProposed(logger log.Log, nodeID types.NodeID) error {
	if _, ok := s.hasProposed[nodeID]; ok {
		// see TODOs for registerVoted()
		logger.Warning("already received proposal from miner")
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
