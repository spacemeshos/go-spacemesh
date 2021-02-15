package tortoisebeacon

import "github.com/spacemeshos/go-spacemesh/common/types"

const (
	hDist = 1 // TODO: change
	tAve  = 1 // TODO: change
)

func TortoiseCore(distance int, localVotes map[types.ATXID]bool, voteWeightsFor,
	voteWeightsAgainst []types.ATXID, weakCoin bool) (valid map[types.ATXID]bool, grades map[types.ATXID]int) {

	valid = make(map[types.ATXID]bool)
	grades = make(map[types.ATXID]int)

	if distance < hDist {
		for atxID, vote := range localVotes {
			valid[atxID] = vote
			grades[atxID] = 1 / threshold()
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

		g := diff / threshold()
		if votesFor > threshold() {
			valid[atxID] = true
			grades[atxID] = g
		} else if votesAgainst > threshold() {
			valid[atxID] = false
			grades[atxID] = g
		} else {
			valid[atxID] = weakCoin
			grades[atxID] = 0
		}
	}

	for atxID, votesAgainst := range voteWeightsAgainstMap {
		g := -votesAgainst / threshold()
		if votesAgainst > threshold() {
			valid[atxID] = false
			grades[atxID] = g
		} else {
			valid[atxID] = weakCoin
			grades[atxID] = 0
		}
	}

	return valid, grades
}

func threshold() int {
	return theta * tAve
}
