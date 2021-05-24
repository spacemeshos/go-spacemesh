package tortoisebeacon

import "github.com/bits-and-blooms/bitset"

func (tb *TortoiseBeacon) encodeVotes(currentRound votesSetPair, firstRound firstRoundVotes) (votesBitVector []uint64) {
	validVotes := firstRound.ValidVotes
	potentiallyValidVotes := firstRound.PotentiallyValidVotes
	length := uint(len(validVotes) + len(potentiallyValidVotes))

	if len(validVotes) > tb.config.VotesLimit {
		validVotes = validVotes[:tb.config.VotesLimit]
		potentiallyValidVotes = potentiallyValidVotes[:0]
		length = uint(tb.config.VotesLimit)
	}

	if length > uint(tb.config.VotesLimit) {
		potentiallyValidVotes = potentiallyValidVotes[:tb.config.VotesLimit-len(validVotes)]
		length = uint(tb.config.VotesLimit)
	}

	bs := bitset.New(length)
	for i, v := range validVotes {
		if _, ok := currentRound.ValidVotes[v]; ok {
			bs.Set(uint(i))
		}
		if _, ok := currentRound.InvalidVotes[v]; ok {
			bs.Clear(uint(i))
		}
	}

	offset := len(validVotes)
	for i, v := range potentiallyValidVotes {
		if _, ok := currentRound.ValidVotes[v]; ok {
			bs.Set(uint(offset + i))
		}
		if _, ok := currentRound.InvalidVotes[v]; ok {
			bs.Clear(uint(offset + i))
		}
	}

	return bs.Bytes()
}
