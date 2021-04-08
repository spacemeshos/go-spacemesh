package tortoisebeacon

type votesSetPair struct {
	VotesFor     hashSet
	VotesAgainst hashSet
}

// Diff calculates difference delta from originRound to currentRound.
func (currentRound votesSetPair) Diff(originRound votesSetPair) (votesFor, votesAgainst hashList) {
	votesForDiff := make(hashList, 0)
	votesAgainstDiff := make(hashList, 0)

	for vote := range currentRound.VotesFor {
		if _, ok := originRound.VotesFor[vote]; !ok {
			votesForDiff = append(votesForDiff, vote)
		}
	}

	for vote := range currentRound.VotesAgainst {
		if _, ok := originRound.VotesAgainst[vote]; !ok {
			votesAgainstDiff = append(votesAgainstDiff, vote)
		}
	}

	return votesForDiff, votesAgainstDiff
}
