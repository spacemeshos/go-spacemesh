package beacon

import "github.com/bits-and-blooms/bitset"

func encodeVotes(currentRound allVotes, firstRound proposals, voteLimit uint64) (votesBitVector []uint64) {
	validVotes := firstRound.valid
	potentiallyValidVotes := firstRound.potentiallyValid
	length := uint(len(validVotes) + len(potentiallyValidVotes))

	if uint64(len(validVotes)) > voteLimit {
		validVotes = validVotes[:voteLimit]
		potentiallyValidVotes = potentiallyValidVotes[:0]
		length = uint(voteLimit)
	}

	if length > uint(voteLimit) {
		potentiallyValidVotes = potentiallyValidVotes[:voteLimit-uint64(len(validVotes))]
		length = uint(voteLimit)
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

func decodeVotes(votesBitVector []uint64, firstRound proposals) allVotes {
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
