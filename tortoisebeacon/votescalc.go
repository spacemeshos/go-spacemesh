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

func (tb *TortoiseBeacon) calcVotes(epoch types.EpochID, round types.RoundID) votesSetPair {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	votesCount := tb.firstRoundVotes(epoch)

	tb.Log.With().Info("Calculated first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("votesCount", fmt.Sprint(votesCount)))

	ownFirstRoundVotes := tb.calcOwnFirstRoundVotes(epoch, votesCount)

	tb.Log.With().Info("Calculated own first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("votesCount", fmt.Sprint(votesCount)),
		log.String("ownFirstRoundVotes", fmt.Sprint(ownFirstRoundVotes)))

	tb.calcVotesCount(epoch, round, votesCount)

	tb.Log.With().Info("Calculated votes count",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("votesCount", fmt.Sprint(votesCount)))

	ownCurrentRoundVotes := tb.calcOwnCurrentRoundVotes(epoch, round, votesCount)

	tb.Log.With().Info("Calculated votes for one round",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("for", fmt.Sprint(ownCurrentRoundVotes.ValidVotes)),
		log.String("against", fmt.Sprint(ownCurrentRoundVotes.InvalidVotes)))

	return ownCurrentRoundVotes
}

func (tb *TortoiseBeacon) firstRoundVotes(epoch types.EpochID) votesCountMap {
	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	// protected by tb.votesMu
	firstRoundIncomingVotes := tb.incomingVotes[firstRoundInThisEpoch]

	firstRoundVotesCount := make(map[proposal]int)

	// TODO(nkryuchkov): update a vote margin

	for nodeID, votesList := range firstRoundIncomingVotes {
		firstRoundVotesFor := make(hashSet)
		firstRoundVotesAgainst := make(hashSet)

		for vote := range votesList.ValidVotes {
			firstRoundVotesCount[vote] += tb.voteWeight(nodeID)
			firstRoundVotesFor[vote] = struct{}{}
		}

		for vote := range votesList.InvalidVotes {
			firstRoundVotesCount[vote] -= tb.voteWeight(nodeID)
			firstRoundVotesAgainst[vote] = struct{}{}
		}
	}

	// TODO(nkryuchkov): Unused for now.
	// protected by tb.votesMu
	tb.votesCountCache[firstRoundInThisEpoch] = make(map[proposal]int)
	for k, v := range firstRoundVotesCount {
		tb.votesCountCache[firstRoundInThisEpoch][k] = v
	}

	return firstRoundVotesCount
}

func (tb *TortoiseBeacon) calcOwnFirstRoundVotes(epoch types.EpochID, votesCount votesCountMap) votesSetPair {
	ownFirstRoundsVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	// TODO(nkryuchkov): we don't need to use weak coin in the 1st round, because no votes to count: only timely things, because 1st votes are different than other rounds
	for vote, count := range votesCount {
		switch {
		case count >= tb.votingThreshold():
			ownFirstRoundsVotes.ValidVotes[vote] = struct{}{}
		case count <= -tb.votingThreshold():
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

	// TODO(nkryuchkov): as pointer is shared, ensure that maps are not changed
	tb.ownVotes[firstRoundInThisEpoch] = ownFirstRoundsVotes

	return ownFirstRoundsVotes
}

func (tb *TortoiseBeacon) calcVotesCount(epoch types.EpochID, upToRound types.RoundID, votesCount votesCountMap) {
	for round := firstRound + 1; round <= upToRound; round++ {
		thisRound := epochRoundPair{
			EpochID: epoch,
			Round:   round,
		}

		thisRoundVotes := tb.incomingVotes[thisRound]

		for pk, votesList := range thisRoundVotes {
			for vote := range votesList.ValidVotes {
				votesCount[vote] += tb.voteWeight(pk)
			}

			for vote := range votesList.InvalidVotes {
				votesCount[vote] -= tb.voteWeight(pk)
			}
		}
	}
}

// TODO(nkryuchkov): rename votesCount to votesMargin
func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(epoch types.EpochID, round types.RoundID, votesMargin votesCountMap) votesSetPair {
	ownCurrentRoundVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	currentRound := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	// TODO(nkryuchkov): as pointer is shared, ensure that maps are not modified
	tb.votesCountCache[currentRound] = votesMargin

	// TODO(nkryuchkov): should happen after weak coin for this round is calculated; consider calculating in two steps
	for vote, weightCount := range votesMargin {
		switch {
		case weightCount >= tb.votingThreshold():
			ownCurrentRoundVotes.ValidVotes[vote] = struct{}{}
		case weightCount <= -tb.votingThreshold():
			ownCurrentRoundVotes.InvalidVotes[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.ValidVotes[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.InvalidVotes[vote] = struct{}{}
		}
	}

	tb.ownVotes[currentRound] = ownCurrentRoundVotes

	return ownCurrentRoundVotes
}
