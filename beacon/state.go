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
	hasVoted                  []map[types.NodeID]struct{}
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
		logger:                  logger,
		epochWeight:             epochWeight,
		nonce:                   nonce,
		minerAtxs:               miners,
		firstRoundIncomingVotes: make(map[types.NodeID]proposalList),
		votesMargin:             map[Proposal]*big.Int{},
		hasProposed:             make(map[types.NodeID]struct{}),
		hasVoted:                make([]map[types.NodeID]struct{}, cfg.RoundsNumber),
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

func (s *state) setMinerFirstRoundVote(minerID types.NodeID, voteList []Proposal) {
	s.firstRoundIncomingVotes[minerID] = voteList
}

func (s *state) getMinerFirstRoundVote(minerID types.NodeID) (proposalList, error) {
	p, ok := s.firstRoundIncomingVotes[minerID]
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

func (s *state) registerVoted(logger log.Log, minerID types.NodeID, round types.RoundID) error {
	if s.hasVoted[round] == nil {
		s.hasVoted[round] = make(map[types.NodeID]struct{})
	}

	// TODO(nkryuchkov): consider having a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := s.hasVoted[round][minerID]; ok {
		logger.Warning("already received vote from miner for this round")

		// TODO(nkryuchkov): report this miner through gossip
		// TODO(nkryuchkov): store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO(nkryuchkov): handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO(nkryuchkov): blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO(nkryuchkov): ban id forever globally across packages since this epoch
		// TODO(nkryuchkov): (not specific to beacon) do the same for ATXs

		return fmt.Errorf("[round %v] already voted (miner ID %v): %w", round, minerID.ShortString(), errAlreadyVoted)
	}

	s.hasVoted[round][minerID] = struct{}{}
	return nil
}
