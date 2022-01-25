package beacon

import "github.com/bits-and-blooms/bitset"

func (pd *ProtocolDriver) encodeVotes(currentRound allVotes, firstRound proposals) (votesBitVector []uint64) {
	validVotes := firstRound.valid
	potentiallyValidVotes := firstRound.potentiallyValid
	length := uint(len(validVotes) + len(potentiallyValidVotes))

	if uint64(len(validVotes)) > pd.config.VotesLimit {
		validVotes = validVotes[:pd.config.VotesLimit]
		potentiallyValidVotes = potentiallyValidVotes[:0]
		length = uint(pd.config.VotesLimit)
	}

	if length > uint(pd.config.VotesLimit) {
		potentiallyValidVotes = potentiallyValidVotes[:pd.config.VotesLimit-uint64(len(validVotes))]
		length = uint(pd.config.VotesLimit)
	}

	bs := bitset.New(length)

	// TODO(nkryuchkov): fix data race
	for i, v := range validVotes {
		if _, ok := currentRound.valid[string(v)]; ok {
			bs.Set(uint(i))
		}
		if _, ok := currentRound.invalid[string(v)]; ok {
			bs.Clear(uint(i))
		}
	}

	offset := len(validVotes)

	for i, v := range potentiallyValidVotes {
		if _, ok := currentRound.valid[string(v)]; ok {
			bs.Set(uint(offset + i))
		}
		if _, ok := currentRound.invalid[string(v)]; ok {
			bs.Clear(uint(offset + i))
		}
	}

	return bs.Bytes()
}
