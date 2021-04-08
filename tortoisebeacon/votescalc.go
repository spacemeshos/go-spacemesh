package tortoisebeacon

import (
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

func (tb *TortoiseBeacon) calcVotesFromProposals(epoch types.EpochID) (votesFor, votesAgainst hashList) {
	votesFor = make(hashList, 0)
	votesAgainst = make(hashList, 0)

	stringVotesFor := make([]string, 0)
	stringVotesAgainst := make([]string, 0)

	tb.timelyProposalsMu.RLock()
	timelyProposals := tb.timelyProposals[epoch]
	tb.timelyProposalsMu.RUnlock()

	tb.delayedProposalsMu.Lock()
	delayedProposals := tb.delayedProposals[epoch]
	tb.delayedProposalsMu.Unlock()

	for p := range timelyProposals {
		votesFor = append(votesFor, p)
		stringVotesFor = append(stringVotesFor, p.String())
	}

	for p := range delayedProposals {
		votesAgainst = append(votesAgainst, p)
		stringVotesAgainst = append(stringVotesAgainst, p.String())
	}

	tb.Log.With().Info("Calculated votes from proposals",
		log.Uint64("epoch", uint64(epoch)),
		log.String("for", strings.Join(stringVotesFor, ", ")),
		log.String("against", strings.Join(stringVotesAgainst, ", ")))

	return votesFor, votesAgainst
}

func (tb *TortoiseBeacon) calcVotesDelta(epoch types.EpochID, round types.RoundID) (forDiff, againstDiff hashList) {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	votesCount := tb.countFirstRoundVotes(epoch)
	ownFirstRoundVotes := tb.calcOwn1stRoundVotes(epoch, votesCount)
	tb.calcVotesCount(epoch, round, votesCount)
	ownCurrentRoundVotes := tb.calcOwnCurrentRoundVotes(epoch, round, ownFirstRoundVotes, votesCount)

	return tb.calcOwnCurrentRoundVotesDiff(epoch, round, ownCurrentRoundVotes, ownFirstRoundVotes)
}

func (tb *TortoiseBeacon) countFirstRoundVotes(epoch types.EpochID) votesCountMap {
	firstRoundInThisEpoch := epochRoundPair{
		EpochID: epoch,
		Round:   1,
	}

	tb.votesCache[firstRoundInThisEpoch] = make(map[p2pcrypto.PublicKey]votes)

	firstRoundVotes := tb.incomingVotes[firstRoundInThisEpoch]
	// TODO(nkryuchkov): as pointer is shared, ensure that maps are not changed
	tb.votesCache[firstRoundInThisEpoch] = firstRoundVotes

	votesCount := make(map[types.Hash32]int)

	for pk, votesList := range firstRoundVotes {
		firstRoundVotesFor := make(votesSet)
		firstRoundVotesAgainst := make(votesSet)

		for vote := range votesList.votesFor {
			votesCount[vote] += tb.voteWeight(pk)
			firstRoundVotesFor[vote] = struct{}{}
		}

		for vote := range votesList.votesAgainst {
			votesCount[vote] -= tb.voteWeight(pk)
			firstRoundVotesAgainst[vote] = struct{}{}
		}

		// copy to cache
		tb.votesCache[firstRoundInThisEpoch][pk] = votes{
			votesFor:     firstRoundVotesFor,
			votesAgainst: firstRoundVotesAgainst,
		}
	}

	tb.votesCountCache[firstRoundInThisEpoch] = make(map[types.Hash32]int)
	for k, v := range tb.votesCache[firstRoundInThisEpoch] {
		tb.votesCache[firstRoundInThisEpoch][k] = v
	}

	return votesCount
}

func (tb *TortoiseBeacon) calcOwn1stRoundVotes(epoch types.EpochID, votesCount votesCountMap) votes {
	ownFirstRoundsVotes := votes{
		votesFor:     make(votesSet),
		votesAgainst: make(votesSet),
	}

	for vote, count := range votesCount {
		switch {
		case count > tb.threshold():
			ownFirstRoundsVotes.votesFor[vote] = struct{}{}
		case count < -tb.threshold():
			ownFirstRoundsVotes.votesAgainst[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, 1):
			ownFirstRoundsVotes.votesFor[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, 1):
			ownFirstRoundsVotes.votesAgainst[vote] = struct{}{}
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
	for round := firstRound + 1; round < upToRound; round++ {
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
			for vote := range votesList.votesFor {
				votesCount[vote] += tb.voteWeight(pk)
			}

			for vote := range votesList.votesAgainst {
				votesCount[vote] -= tb.voteWeight(pk)
			}
		}
	}
}

func (tb *TortoiseBeacon) calcOneRoundVotes(epoch types.EpochID, round types.RoundID) votesPerPK {
	thisRoundVotes := tb.copyFirstRoundVotes(epoch)

	thisRound := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	thisRoundVotesDiff := tb.incomingVotes[thisRound]
	// TODO(nkryuchkov): consider caching to avoid recalculating votes
	for pk, votesDiff := range thisRoundVotesDiff {
		for vote := range votesDiff.votesFor {
			if m := thisRoundVotes[pk].votesAgainst; m != nil {
				delete(thisRoundVotes[pk].votesAgainst, vote)
			}

			if m := thisRoundVotes[pk].votesFor; m != nil {
				thisRoundVotes[pk].votesFor[vote] = struct{}{}
			}
		}

		for vote := range votesDiff.votesAgainst {
			if m := thisRoundVotes[pk].votesFor; m != nil {
				delete(thisRoundVotes[pk].votesFor, vote)
			}

			if m := thisRoundVotes[pk].votesAgainst; m != nil {
				thisRoundVotes[pk].votesAgainst[vote] = struct{}{}
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
		votesForCopy := make(votesSet)
		votesAgainstCopy := make(votesSet)

		for k, v := range votesList.votesFor {
			votesForCopy[k] = v
		}

		for k, v := range votesList.votesAgainst {
			votesAgainstCopy[k] = v
		}

		thisRoundVotes[pk] = votes{
			votesFor:     votesForCopy,
			votesAgainst: votesAgainstCopy,
		}
	}

	return thisRoundVotes
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(
	epoch types.EpochID,
	round types.RoundID,
	ownFirstRoundVotes votes,
	votesCount votesCountMap,
) votes {
	ownCurrentRoundVotes := votes{
		votesFor:     make(votesSet),
		votesAgainst: make(votesSet),
	}

	currentRound := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	tb.ownVotes[currentRound] = ownFirstRoundVotes
	// TODO(nkryuchkov): as pointer is shared, ensure that maps are not modified
	tb.votesCountCache[currentRound] = votesCount

	for vote, count := range votesCount {
		switch {
		case count > tb.threshold():
			ownCurrentRoundVotes.votesFor[vote] = struct{}{}
		case count < -tb.threshold():
			ownCurrentRoundVotes.votesAgainst[vote] = struct{}{}
		case tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.votesFor[vote] = struct{}{}
		case !tb.weakCoin.Get(epoch, round):
			ownCurrentRoundVotes.votesAgainst[vote] = struct{}{}
		}
	}

	return ownCurrentRoundVotes
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotesDiff(
	epoch types.EpochID,
	round types.RoundID,
	ownCurrentRoundVotes,
	ownFirstRoundsVotes votes,
) (
	votesForDiff,
	votesAgainstDiff hashList,
) {
	votesForDiff = make(hashList, 0)
	votesAgainstDiff = make(hashList, 0)

	stringVotesFor := make([]string, 0, len(ownCurrentRoundVotes.votesFor))
	stringVotesAgainst := make([]string, 0, len(ownCurrentRoundVotes.votesAgainst))

	stringVotesForDiff := make([]string, 0)
	stringVotesAgainstDiff := make([]string, 0)

	for vote := range ownCurrentRoundVotes.votesFor {
		if _, ok := ownFirstRoundsVotes.votesFor[vote]; !ok {
			votesForDiff = append(votesForDiff, vote)
			stringVotesForDiff = append(stringVotesForDiff, vote.String())
		}

		stringVotesFor = append(stringVotesFor, vote.String())
	}

	for vote := range ownCurrentRoundVotes.votesAgainst {
		if _, ok := ownFirstRoundsVotes.votesAgainst[vote]; !ok {
			votesAgainstDiff = append(votesAgainstDiff, vote)
			stringVotesAgainstDiff = append(stringVotesAgainstDiff, vote.String())
		}

		stringVotesAgainst = append(stringVotesAgainst, vote.String())
	}

	tb.Log.With().Info("Calculated votes for one round",
		log.Uint64("epoch", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("for", strings.Join(stringVotesFor, ", ")),
		log.String("against", strings.Join(stringVotesAgainst, ", ")))

	tb.Log.With().Info("Calculated votes diff for one round",
		log.Uint64("epoch", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.String("for", strings.Join(stringVotesForDiff, ", ")),
		log.String("against", strings.Join(stringVotesAgainstDiff, ", ")))

	return votesForDiff, votesAgainstDiff
}
