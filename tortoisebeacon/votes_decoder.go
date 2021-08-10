package tortoisebeacon

import "github.com/bits-and-blooms/bitset"

func (tb *TortoiseBeacon) decodeVotes(votesBitVector []uint64, firstRound proposals) votesSetPair {
	result := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	for _, vote := range firstRound.ValidProposals {
		result.ValidVotes[vote] = struct{}{}
	}

	for _, vote := range firstRound.PotentiallyValidProposals {
		result.InvalidVotes[vote] = struct{}{}
	}

	bs := bitset.From(votesBitVector)

	for i := 0; i < len(firstRound.ValidProposals); i++ {
		if !bs.Test(uint(i)) {
			delete(result.ValidVotes, firstRound.ValidProposals[i])

			if _, ok := result.InvalidVotes[firstRound.ValidProposals[i]]; !ok {
				result.InvalidVotes[firstRound.ValidProposals[i]] = struct{}{}
			}
		}
	}

	offset := len(firstRound.ValidProposals)
	for i := 0; i < len(firstRound.PotentiallyValidProposals); i++ {
		if bs.Test(uint(offset + i)) {
			delete(result.InvalidVotes, firstRound.PotentiallyValidProposals[i])
			if _, ok := result.ValidVotes[firstRound.PotentiallyValidProposals[i]]; !ok {
				result.ValidVotes[firstRound.PotentiallyValidProposals[i]] = struct{}{}
			}
		}
	}

	return result
}
