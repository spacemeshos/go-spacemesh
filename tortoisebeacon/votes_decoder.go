package tortoisebeacon

import "github.com/bits-and-blooms/bitset"

func (tb *TortoiseBeacon) decodeVotes(votesBitVector []uint64, firstRound proposalsBytes) allVotes {
	result := allVotes{
		valid:   make(proposalSet),
		invalid: make(proposalSet),
	}

	for _, vote := range firstRound.ValidProposals {
		result.valid[string(vote)] = struct{}{}
	}

	for _, vote := range firstRound.PotentiallyValidProposals {
		result.invalid[string(vote)] = struct{}{}
	}

	bs := bitset.From(votesBitVector)

	for i := 0; i < len(firstRound.ValidProposals); i++ {
		if !bs.Test(uint(i)) {
			key := string(firstRound.ValidProposals[i])
			delete(result.valid, key)

			if _, ok := result.invalid[key]; !ok {
				result.invalid[key] = struct{}{}
			}
		}
	}

	offset := len(firstRound.ValidProposals)
	for i := 0; i < len(firstRound.PotentiallyValidProposals); i++ {
		if bs.Test(uint(offset + i)) {
			key := string(firstRound.PotentiallyValidProposals[i])
			delete(result.invalid, key)

			if _, ok := result.valid[key]; !ok {
				result.valid[key] = struct{}{}
			}
		}
	}

	return result
}
