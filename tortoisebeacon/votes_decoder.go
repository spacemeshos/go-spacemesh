package tortoisebeacon

import "github.com/bits-and-blooms/bitset"

func (tb *TortoiseBeacon) decodeVotes(votesBitVector []uint64, firstRound proposalsBytes) votesSetPair {
	result := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	for _, vote := range firstRound.ValidProposals {
		result.ValidVotes[string(vote)] = struct{}{}
	}

	for _, vote := range firstRound.PotentiallyValidProposals {
		result.InvalidVotes[string(vote)] = struct{}{}
	}

	bs := bitset.From(votesBitVector)

	for i := 0; i < len(firstRound.ValidProposals); i++ {
		if !bs.Test(uint(i)) {
			key := string(firstRound.ValidProposals[i])
			delete(result.ValidVotes, key)

			if _, ok := result.InvalidVotes[key]; !ok {
				result.InvalidVotes[key] = struct{}{}
			}
		}
	}

	offset := len(firstRound.ValidProposals)
	for i := 0; i < len(firstRound.PotentiallyValidProposals); i++ {
		if bs.Test(uint(offset + i)) {
			key := string(firstRound.PotentiallyValidProposals[i])
			delete(result.InvalidVotes, key)

			if _, ok := result.ValidVotes[key]; !ok {
				result.ValidVotes[key] = struct{}{}
			}
		}
	}

	return result
}
