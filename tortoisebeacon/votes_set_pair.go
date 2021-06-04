package tortoisebeacon

type votesSetPair struct {
	ValidVotes   hashSet
	InvalidVotes hashSet
}

// Diff calculates difference delta from originRound to currentRound.
func (currentRound votesSetPair) Diff(originRound votesSetPair) (votesFor, votesAgainst proposalList) {
	votesForDiff := make(proposalList, 0)
	votesAgainstDiff := make(proposalList, 0)

	for vote := range currentRound.ValidVotes {
		if _, ok := originRound.ValidVotes[vote]; !ok {
			votesForDiff = append(votesForDiff, vote)
		}
	}

	for vote := range currentRound.InvalidVotes {
		if _, ok := originRound.InvalidVotes[vote]; !ok {
			votesAgainstDiff = append(votesAgainstDiff, vote)
		}
	}

	return votesForDiff.Sort(), votesAgainstDiff.Sort()
}
