package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

func (tb *TortoiseBeacon) calcVotesFromProposals(epoch types.EpochID) (votesFor, votesAgainst hashList) {
	votesFor = make(hashList, 0)
	votesAgainst = make(hashList, 0)

	tb.timelyProposalsMu.RLock()

	timelyProposals := tb.timelyProposals[epoch]

	for p := range timelyProposals {
		votesFor = append(votesFor, p)
	}

	tb.timelyProposalsMu.RUnlock()

	tb.delayedProposalsMu.Lock()

	delayedProposals := tb.delayedProposals[epoch]

	for p := range delayedProposals {
		votesAgainst = append(votesAgainst, p)
	}

	tb.delayedProposalsMu.Unlock()

	tb.Log.With().Info("Calculated votes from proposals",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("for", fmt.Sprint(votesFor)),
		log.String("against", fmt.Sprint(votesAgainst)))

	return votesFor.Sort(), votesAgainst.Sort()
}

func (tb *TortoiseBeacon) calcVotesDelta(epoch types.EpochID, round types.RoundID) (forDiff, againstDiff hashList) {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	votesCount := tb.firstRoundVotes(epoch)

	tb.Log.With().Info("Calculated first round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("incomingVotes", fmt.Sprint(tb.incomingVotes[epochRoundPair{EpochID: epoch, Round: 1}])),
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
		log.String("for", fmt.Sprint(ownCurrentRoundVotes.VotesFor)),
		log.String("against", fmt.Sprint(ownCurrentRoundVotes.VotesAgainst)))

	votesFor, votesAgainst := ownCurrentRoundVotes.Diff(ownFirstRoundVotes)

	tb.Log.With().Info("Calculated votes diff for one round",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("for", fmt.Sprint(votesFor)),
		log.String("against", fmt.Sprint(votesAgainst)))

	return votesFor.Sort(), votesAgainst.Sort()
}

func (tb *TortoiseBeacon) firstRoundVotes(epoch types.EpochID) votesCountMap {
	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	// protected by tb.votesMu
	tb.votesCache[firstRoundInThisEpoch] = make(map[p2pcrypto.PublicKey]votesSetPair)
	firstRoundIncomingVotes := tb.incomingVotes[firstRoundInThisEpoch]
	// TODO(nkryuchkov): as pointer is shared, ensure that maps are not changed
	tb.votesCache[firstRoundInThisEpoch] = firstRoundIncomingVotes

	firstRoundVotesCount := make(map[types.Hash32]int)

	for pk, votesList := range firstRoundIncomingVotes {
		firstRoundVotesFor := make(hashSet)
		firstRoundVotesAgainst := make(hashSet)

		for vote := range votesList.VotesFor {
			firstRoundVotesCount[vote] += tb.voteWeight(pk)
			firstRoundVotesFor[vote] = struct{}{}
		}

		for vote := range votesList.VotesAgainst {
			firstRoundVotesCount[vote] -= tb.voteWeight(pk)
			firstRoundVotesAgainst[vote] = struct{}{}
		}

		// copy to cache
		tb.votesCache[firstRoundInThisEpoch][pk] = votesSetPair{
			VotesFor:     firstRoundVotesFor,
			VotesAgainst: firstRoundVotesAgainst,
		}
	}

	// protected by tb.votesMu
	tb.votesCountCache[firstRoundInThisEpoch] = make(map[types.Hash32]int)
	for k, v := range firstRoundVotesCount {
		tb.votesCountCache[firstRoundInThisEpoch][k] = v
	}

	return firstRoundVotesCount
}

func (tb *TortoiseBeacon) calcOwnFirstRoundVotes(epoch types.EpochID, votesCount votesCountMap) votesSetPair {
	ownFirstRoundsVotes := votesSetPair{
		VotesFor:     make(hashSet),
		VotesAgainst: make(hashSet),
	}

	for vote, count := range votesCount {
		switch {
		case count >= tb.votingThreshold():
			ownFirstRoundsVotes.VotesFor[vote] = struct{}{}
		case count <= -tb.votingThreshold():
			ownFirstRoundsVotes.VotesAgainst[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, 1):
			ownFirstRoundsVotes.VotesFor[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, 1):
			ownFirstRoundsVotes.VotesAgainst[vote] = struct{}{}
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

		var thisRoundVotes votesPerPK
		if cache, ok := tb.votesCache[thisRound]; ok {
			thisRoundVotes = cache
		} else {
			thisRoundVotes = tb.calcOneRoundVotes(epoch, round)
		}

		for pk, votesList := range thisRoundVotes {
			for vote := range votesList.VotesFor {
				votesCount[vote] += tb.voteWeight(pk)
			}

			for vote := range votesList.VotesAgainst {
				votesCount[vote] -= tb.voteWeight(pk)
			}
		}
	}
}

// calcOneRoundVotes takes all votes from the first round and applies the vote difference in the round
// specified by the 'round' parameter.
// The output is all votes (not the difference) calculated for one round referenced by PK.
func (tb *TortoiseBeacon) calcOneRoundVotes(epoch types.EpochID, round types.RoundID) votesPerPK {
	thisRoundVotes := tb.copyFirstRoundVotes(epoch)

	thisRound := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	thisRoundVotesDiff := tb.incomingVotes[thisRound]
	for pk, votesDiff := range thisRoundVotesDiff {
		for vote := range votesDiff.VotesFor {
			if m := thisRoundVotes[pk].VotesAgainst; m != nil {
				delete(thisRoundVotes[pk].VotesAgainst, vote)
			}

			if m := thisRoundVotes[pk].VotesFor; m != nil {
				thisRoundVotes[pk].VotesFor[vote] = struct{}{}
			}
		}

		for vote := range votesDiff.VotesAgainst {
			if m := thisRoundVotes[pk].VotesFor; m != nil {
				delete(thisRoundVotes[pk].VotesFor, vote)
			}

			if m := thisRoundVotes[pk].VotesAgainst; m != nil {
				thisRoundVotes[pk].VotesAgainst[vote] = struct{}{}
			}
		}
	}

	tb.votesCache[thisRound] = thisRoundVotes

	return thisRoundVotes
}

func (tb *TortoiseBeacon) copyFirstRoundVotes(epoch types.EpochID) votesPerPK {
	thisRoundVotes := make(votesPerPK)

	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	firstRoundIncomingVotes := tb.incomingVotes[firstRoundInThisEpoch]
	for pk, votesList := range firstRoundIncomingVotes {
		votesForCopy := make(hashSet)
		votesAgainstCopy := make(hashSet)

		for k, v := range votesList.VotesFor {
			votesForCopy[k] = v
		}

		for k, v := range votesList.VotesAgainst {
			votesAgainstCopy[k] = v
		}

		thisRoundVotes[pk] = votesSetPair{
			VotesFor:     votesForCopy,
			VotesAgainst: votesAgainstCopy,
		}
	}

	return thisRoundVotes
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(epoch types.EpochID, round types.RoundID, votesCount votesCountMap) votesSetPair {
	ownCurrentRoundVotes := votesSetPair{
		VotesFor:     make(hashSet),
		VotesAgainst: make(hashSet),
	}

	currentRound := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	// TODO(nkryuchkov): as pointer is shared, ensure that maps are not modified
	tb.votesCountCache[currentRound] = votesCount

	for vote, count := range votesCount {
		switch {
		case count >= tb.votingThreshold():
			ownCurrentRoundVotes.VotesFor[vote] = struct{}{}
		case count <= -tb.votingThreshold():
			ownCurrentRoundVotes.VotesAgainst[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.VotesFor[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.VotesAgainst[vote] = struct{}{}
		}
	}

	tb.ownVotes[currentRound] = ownCurrentRoundVotes

	return ownCurrentRoundVotes
}
