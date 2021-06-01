package tortoisebeacon

import "github.com/bits-and-blooms/bitset"

func (tb *TortoiseBeacon) decodeVotes(votesBitVector []uint64, firstRound firstRoundVotes) votesSetPair {
	result := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	for _, vote := range firstRound.ValidVotes {
		result.ValidVotes[vote] = struct{}{}
	}

	for _, vote := range firstRound.PotentiallyValidVotes {
		result.InvalidVotes[vote] = struct{}{}
	}

	bs := bitset.From(votesBitVector)

	for i := 0; i < len(firstRound.ValidVotes); i++ {
		if !bs.Test(uint(i)) {
			if _, ok := result.ValidVotes[firstRound.ValidVotes[i]]; ok {
				delete(result.ValidVotes, firstRound.ValidVotes[i])
			}
			if _, ok := result.InvalidVotes[firstRound.ValidVotes[i]]; !ok {
				result.InvalidVotes[firstRound.ValidVotes[i]] = struct{}{}
			}
		}
	}

	offset := len(firstRound.ValidVotes)
	for i := 0; i < len(firstRound.PotentiallyValidVotes); i++ {
		if bs.Test(uint(offset + i)) {
			if _, ok := result.InvalidVotes[firstRound.PotentiallyValidVotes[i]]; ok {
				delete(result.InvalidVotes, firstRound.PotentiallyValidVotes[i])
			}
			if _, ok := result.ValidVotes[firstRound.PotentiallyValidVotes[i]]; !ok {
				result.ValidVotes[firstRound.PotentiallyValidVotes[i]] = struct{}{}
			}
		}
	}

	return result
}
