package beacon

import (
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// state does the data management for epoch specific data for the protocol.
// not thread-safe. it relies on ProtocolDriver's thread-safety mechanism.
type state struct {
	logger      log.Log
	nonce       types.VRFPostIndex
	epochWeight uint64
	atxs        []types.ATXID
	// the original proposals as received, bucketed by validity.
	incomingProposals proposals
	// minerPublicKey -> list of proposal.
	// this list is used in encoding/decoding votes for each miner in all subsequent voting rounds.
	firstRoundIncomingVotes map[string]proposalList
	// TODO(nkryuchkov): For every round excluding first round consider having a vector of opinions.
	votesMargin               map[string]*big.Int
	hasProposed               map[string]struct{}
	hasVoted                  []map[string]struct{}
	proposalPhaseFinishedTime time.Time
	proposalChecker           eligibilityChecker
	unitAllowance             weakcoin.UnitAllowances
}

func newState(logger log.Log, cfg Config, nonce types.VRFPostIndex, epochWeight uint64, atxids []types.ATXID, ua weakcoin.UnitAllowances) *state {
	return &state{
		logger:                  logger,
		epochWeight:             epochWeight,
		nonce:                   nonce,
		unitAllowance:           ua,
		atxs:                    atxids,
		firstRoundIncomingVotes: make(map[string]proposalList),
		votesMargin:             map[string]*big.Int{},
		hasProposed:             make(map[string]struct{}),
		hasVoted:                make([]map[string]struct{}, cfg.RoundsNumber),
		proposalChecker:         createProposalChecker(logger, cfg, len(atxids)),
	}
}

func (s *state) addValidProposal(proposal []byte) {
	if s.incomingProposals.valid == nil {
		s.incomingProposals.valid = make(map[string]struct{})
	}
	p := string(proposal)
	s.incomingProposals.valid[p] = struct{}{}
	s.votesMargin[p] = new(big.Int)
}

func (s *state) addPotentiallyValidProposal(proposal []byte) {
	if s.incomingProposals.potentiallyValid == nil {
		s.incomingProposals.potentiallyValid = make(map[string]struct{})
	}
	p := string(proposal)
	s.incomingProposals.potentiallyValid[p] = struct{}{}
	s.votesMargin[p] = new(big.Int)
}

func (s *state) setMinerFirstRoundVote(minerPK *signing.PublicKey, voteList [][]byte) {
	s.firstRoundIncomingVotes[string(minerPK.Bytes())] = voteList
}

func (s *state) getMinerFirstRoundVote(minerPK *signing.PublicKey) (proposalList, error) {
	p, ok := s.firstRoundIncomingVotes[string(minerPK.Bytes())]
	if !ok {
		return nil, fmt.Errorf("no first round votes for miner %v", minerPK.String())
	}
	return p, nil
}

func (s *state) addVote(proposal string, vote uint, voteWeight *big.Int) {
	if _, ok := s.votesMargin[proposal]; !ok {
		// voteMargin is updated during the proposal phase.
		// ignore votes on proposals not in the original proposals.
		s.logger.With().Warning("ignoring vote for unknown proposal", log.Binary("proposal", []byte(proposal)))
		return
	}
	if vote == up {
		s.votesMargin[proposal].Add(s.votesMargin[proposal], voteWeight)
	} else {
		s.votesMargin[proposal].Sub(s.votesMargin[proposal], voteWeight)
	}
}

func (s *state) registerProposed(logger log.Log, minerPK *signing.PublicKey) error {
	minerID := string(minerPK.Bytes())
	if _, ok := s.hasProposed[minerID]; ok {
		// see TODOs for registerVoted()
		logger.Warning("already received proposal from miner")
		return fmt.Errorf("already made proposal (miner ID %v): %w", minerPK.ShortString(), errAlreadyProposed)
	}

	s.hasProposed[minerID] = struct{}{}
	return nil
}

func (s *state) registerVoted(logger log.Log, minerPK *signing.PublicKey, round types.RoundID) error {
	if s.hasVoted[round] == nil {
		s.hasVoted[round] = make(map[string]struct{})
	}

	minerID := string(minerPK.Bytes())
	// TODO(nkryuchkov): consider having a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := s.hasVoted[round][minerID]; ok {
		logger.Warning("already received vote from miner for this round")

		// TODO(nkryuchkov): report this miner through gossip
		// TODO(nkryuchkov): store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO(nkryuchkov): handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO(nkryuchkov): blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO(nkryuchkov): ban id forever globally across packages since this epoch
		// TODO(nkryuchkov): (not specific to beacon) do the same for ATXs

		return fmt.Errorf("[round %v] already voted (miner ID %v): %w", round, minerPK.ShortString(), errAlreadyVoted)
	}

	s.hasVoted[round][minerID] = struct{}{}
	return nil
}
