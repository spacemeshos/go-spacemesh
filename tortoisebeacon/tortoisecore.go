package tortoisebeacon

import "github.com/spacemeshos/go-spacemesh/common/types"

func (tb *TortoiseBeacon) TortoiseCore(distance int, localVotes map[types.ATXID]bool, voteWeightsFor,
	voteWeightsAgainst []types.ATXID, weakCoin bool) (valid map[types.ATXID]bool, grades map[types.ATXID]int) {

	valid = make(map[types.ATXID]bool)
	grades = make(map[types.ATXID]int)

	if distance < tb.config.HDist {
		for atxID, vote := range localVotes {
			valid[atxID] = vote
			grades[atxID] = 1 / tb.threshold()
		}

		return valid, grades
	}

	voteWeightsForMap := make(map[types.ATXID]int)
	for _, atxID := range voteWeightsFor {
		voteWeightsForMap[atxID]++
	}

	voteWeightsAgainstMap := make(map[types.ATXID]int)
	for _, atxID := range voteWeightsAgainst {
		voteWeightsAgainstMap[atxID]++
	}

	for atxID, votesFor := range voteWeightsForMap {
		diff := votesFor
		votesAgainst, ok := voteWeightsAgainstMap[atxID]
		if ok {
			diff -= votesAgainst
			delete(voteWeightsAgainstMap, atxID)
		}

		g := diff / tb.threshold()
		if votesFor > tb.threshold() {
			valid[atxID] = true
			grades[atxID] = g
		} else if votesAgainst > tb.threshold() {
			valid[atxID] = false
			grades[atxID] = g
		} else {
			valid[atxID] = weakCoin
			grades[atxID] = 0
		}
	}

	for atxID, votesAgainst := range voteWeightsAgainstMap {
		g := -votesAgainst / tb.threshold()
		if votesAgainst > tb.threshold() {
			valid[atxID] = false
			grades[atxID] = g
		} else {
			valid[atxID] = weakCoin
			grades[atxID] = 0
		}
	}

	return valid, grades
}
