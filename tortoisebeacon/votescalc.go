package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotesFromProposals(epoch types.EpochID) firstRoundVotes {
	valid := make(proposalList, 0)
	potentiallyValid := make(proposalList, 0)

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

	tb.Log.With().Info("Calculated votes from proposals",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("for", fmt.Sprint(valid)),
		log.String("against", fmt.Sprint(potentiallyValid)))

	votes := firstRoundVotes{
		ValidVotes:            valid,
		PotentiallyValidVotes: potentiallyValid,
	}

	tb.firstRoundOutcomingVotes[epoch] = votes

	return votes
}

func (tb *TortoiseBeacon) calcVotes(epoch types.EpochID, round types.RoundID) (votesSetPair, error) {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	votesMargin, err := tb.firstRoundVotes(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc first round votes: %w", err)
	}

	tb.Log.With().Info("Calculated first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("votesMargin", fmt.Sprint(votesMargin)))

	ownFirstRoundVotes, err := tb.calcOwnFirstRoundVotes(epoch, votesMargin)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc own first round votes: %w", err)
	}

	tb.Log.With().Info("Calculated own first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("votesMargin", fmt.Sprint(votesMargin)),
		log.String("ownFirstRoundVotes", fmt.Sprint(ownFirstRoundVotes)))

	if err := tb.calcVotesMargin(epoch, round, votesMargin); err != nil {
		return votesSetPair{}, fmt.Errorf("calc votes count: %w", err)
	}

	tb.Log.With().Info("Calculated votes count",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("votesMargin", fmt.Sprint(votesMargin)))

	ownCurrentRoundVotes, err := tb.calcOwnCurrentRoundVotes(epoch, round, votesMargin)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc own current round votes: %w", err)
	}

	tb.Log.With().Info("Calculated votes for one round",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
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

	firstRoundVotesMargin := make(map[proposal]int)

	for nodeID, votesList := range firstRoundIncomingVotes {
		firstRoundVotesFor := make(hashSet)
		firstRoundVotesAgainst := make(hashSet)

		voteWeight, err := tb.voteWeight(nodeID, epoch)
		if err != nil {
			return nil, fmt.Errorf("get vote weight: %w", err)
		}

		for vote := range votesList.ValidVotes {
			firstRoundVotesMargin[vote] += int(voteWeight)
			firstRoundVotesFor[vote] = struct{}{}
		}

		for vote := range votesList.InvalidVotes {
			firstRoundVotesMargin[vote] -= int(voteWeight)
			firstRoundVotesAgainst[vote] = struct{}{}
		}
	}

	return firstRoundVotesMargin, nil
}

func (tb *TortoiseBeacon) calcOwnFirstRoundVotes(epoch types.EpochID, votesMargin votesMarginMap) (votesSetPair, error) {
	ownFirstRoundsVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	votingThreshold, err := tb.votingThreshold(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("voting threshold: %w", err)
	}

	for vote, margin := range votesMargin {
		switch {
		case margin >= votingThreshold:
			ownFirstRoundsVotes.ValidVotes[vote] = struct{}{}
		case margin <= -votingThreshold:
			ownFirstRoundsVotes.InvalidVotes[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, 1):
			ownFirstRoundsVotes.ValidVotes[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, 1):
			ownFirstRoundsVotes.InvalidVotes[vote] = struct{}{}
		}
	}

	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	tb.ownVotes[firstRoundInThisEpoch] = ownFirstRoundsVotes

	return ownFirstRoundsVotes, nil
}

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
				votesMargin[vote] += int(voteWeight)
			}

			for vote := range votesList.InvalidVotes {
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

	votingThreshold, err := tb.votingThreshold(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("voting threshold: %w", err)
	}

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
