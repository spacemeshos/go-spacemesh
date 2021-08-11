package tortoisebeacon

import "github.com/bits-and-blooms/bitset"

func (tb *TortoiseBeacon) decodeVotes(votesBitVector []uint64, firstRound proposals) allVotes {
	result := allVotes{
		valid:   make(proposalSet),
		invalid: make(proposalSet),
	}

	for _, vote := range firstRound.valid {
		result.valid[string(vote)] = struct{}{}
	}

	for _, vote := range firstRound.potentiallyValid {
		result.invalid[string(vote)] = struct{}{}
	}

	bs := bitset.From(votesBitVector)

	for i := 0; i < len(firstRound.valid); i++ {
		if !bs.Test(uint(i)) {
			key := string(firstRound.valid[i])
			delete(result.valid, key)

			if _, ok := result.invalid[key]; !ok {
				result.invalid[key] = struct{}{}
			}
		}
	}

	offset := len(firstRound.valid)
	for i := 0; i < len(firstRound.potentiallyValid); i++ {
		if bs.Test(uint(offset + i)) {
			key := string(firstRound.potentiallyValid[i])
			delete(result.invalid, key)

			if _, ok := result.valid[key]; !ok {
				result.valid[key] = struct{}{}
			}
		}
	}

	return result
}
