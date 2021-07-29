package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotesFromProposals(epoch types.EpochID) firstRoundVotes {
	valid := make(proposalList, 0)
	potentiallyValid := make(proposalList, 0)

	// TODO: have a mixed list of all sorted proposals
	// have one bit vector: valid proposals

	tb.validProposalsMu.RLock()

	for p := range tb.validProposals[epoch] {
		valid = append(valid, p)
	}

	tb.validProposalsMu.RUnlock()

	tb.potentiallyValidProposalsMu.Lock()

	for p := range tb.potentiallyValidProposals[epoch] {
		potentiallyValid = append(potentiallyValid, p)
	}

	tb.potentiallyValidProposalsMu.Unlock()

	tb.Log.With().Debug("Calculated votes from proposals",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("for", fmt.Sprint(valid)),
		log.String("against", fmt.Sprint(potentiallyValid)))

	votes := firstRoundVotes{
		ValidVotes:            valid,
		PotentiallyValidVotes: potentiallyValid,
	}

	// TODO: also send a bit vector
	// TODO: initialize margin vector to initial votes
	// TODO: use weight
	tb.firstRoundOutcomingVotes[epoch] = votes

	return votes
}

func (tb *TortoiseBeacon) calcVotes(epoch types.EpochID, round types.RoundID) (votesSetPair, error) {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	// TODO: initialize votes margin when we create a proposal list
	votesMargin, err := tb.firstRoundVotes(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc first round votes: %w", err)
	}

	tb.Log.With().Debug("Calculated first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("votesMargin", fmt.Sprint(votesMargin)))

	ownFirstRoundVotes, err := tb.calcOwnFirstRoundVotes(epoch, votesMargin)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc own first round votes: %w", err)
	}

	tb.Log.With().Debug("Calculated own first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("votesMargin", fmt.Sprint(votesMargin)),
		log.String("ownFirstRoundVotes", fmt.Sprint(ownFirstRoundVotes)))

	if err := tb.calcVotesMargin(epoch, round, votesMargin); err != nil {
		return votesSetPair{}, fmt.Errorf("calc votes count: %w", err)
	}

	tb.Log.With().Debug("Calculated votes count",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("votesMargin", fmt.Sprint(votesMargin)))

	ownCurrentRoundVotes, err := tb.calcOwnCurrentRoundVotes(epoch, round, votesMargin)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc own current round votes: %w", err)
	}

	tb.Log.With().Debug("Calculated votes for one round",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("for", fmt.Sprint(ownCurrentRoundVotes.ValidVotes)),
		log.String("against", fmt.Sprint(ownCurrentRoundVotes.InvalidVotes)))

	return ownCurrentRoundVotes, nil
}

func (tb *TortoiseBeacon) firstRoundVotes(epoch types.EpochID) (votesMarginMap, error) {
	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	// protected by tb.votesMu
	firstRoundIncomingVotes := tb.incomingVotes[firstRoundInThisEpoch]

	tb.Log.With().Debug("First round incoming votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(1)),
		log.String("votes", fmt.Sprint(firstRoundIncomingVotes)))

	firstRoundVotesMargin := make(map[proposal]int)

	for nodeID, votesList := range firstRoundIncomingVotes {
		firstRoundVotesFor := make(hashSet)
		firstRoundVotesAgainst := make(hashSet)

		voteWeight, err := tb.voteWeight(nodeID, epoch)
		if err != nil {
			return nil, fmt.Errorf("get vote weight: %w", err)
		}

		for vote := range votesList.ValidVotes {
			// TODO(nkryuchkov): handle overflow
			firstRoundVotesMargin[vote] += int(voteWeight)
			firstRoundVotesFor[vote] = struct{}{}
		}

		for vote := range votesList.InvalidVotes {
			// TODO(nkryuchkov): handle negative overflow
			firstRoundVotesMargin[vote] -= int(voteWeight)
			firstRoundVotesAgainst[vote] = struct{}{}
		}
	}

	tb.Log.With().Debug("First round votes margin",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(1)),
		log.String("votes_margin", fmt.Sprint(firstRoundVotesMargin)))

	return firstRoundVotesMargin, nil
}

// TODO: For every round excluding first round consider having a vector of opinions.
func (tb *TortoiseBeacon) calcOwnFirstRoundVotes(epoch types.EpochID, votesMargin votesMarginMap) (votesSetPair, error) {
	ownFirstRoundsVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("get epoch weight: %w", err)
	}

	votingThreshold := tb.votingThreshold(epochWeight)
	coinToss := tb.weakCoin.Get(epoch, 1)

	for vote, margin := range votesMargin {
		switch {
		case margin >= votingThreshold:
			ownFirstRoundsVotes.ValidVotes[vote] = struct{}{}
		case margin <= -votingThreshold:
			ownFirstRoundsVotes.InvalidVotes[vote] = struct{}{}
		default:
			if coinToss {
				ownFirstRoundsVotes.ValidVotes[vote] = struct{}{}
			} else {
				ownFirstRoundsVotes.InvalidVotes[vote] = struct{}{}
			}
		}
	}

	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	tb.ownVotes[firstRoundInThisEpoch] = ownFirstRoundsVotes

	return ownFirstRoundsVotes, nil
}

// TODO: Same calculation, do incremental part on receiving (vector of margins).
func (tb *TortoiseBeacon) calcVotesMargin(epoch types.EpochID, upToRound types.RoundID, votesMargin votesMarginMap) error {
	for round := firstRound + 1; round <= upToRound; round++ {
		thisRound := epochRoundPair{
			EpochID: epoch,
			Round:   round,
		}

		thisRoundVotes := tb.incomingVotes[thisRound]

		for pk, votesList := range thisRoundVotes {
			voteWeight, err := tb.voteWeight(pk, epoch)
			if err != nil {
				return fmt.Errorf("vote weight: %w", err)
			}

			for vote := range votesList.ValidVotes {
				// TODO(nkryuchkov): handle overflow
				votesMargin[vote] += int(voteWeight)
			}

			for vote := range votesList.InvalidVotes {
				// TODO(nkryuchkov): handle negative overflow
				votesMargin[vote] -= int(voteWeight)
			}
		}
	}

	return nil
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(epoch types.EpochID, round types.RoundID, votesMargin votesMarginMap) (votesSetPair, error) {
	ownCurrentRoundVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	currentRound := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("get epoch weight: %w", err)
	}

	votingThreshold := tb.votingThreshold(epochWeight)

	// TODO(nkryuchkov): should happen after weak coin for this round is calculated; consider calculating in two steps
	for vote, weightCount := range votesMargin {
		switch {
		case weightCount >= votingThreshold:
			ownCurrentRoundVotes.ValidVotes[vote] = struct{}{}
		case weightCount <= -votingThreshold:
			ownCurrentRoundVotes.InvalidVotes[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.ValidVotes[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.InvalidVotes[vote] = struct{}{}
		}
	}

	tb.ownVotes[currentRound] = ownCurrentRoundVotes

	return ownCurrentRoundVotes, nil
}
