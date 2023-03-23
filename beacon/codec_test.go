package beacon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_codec(t *testing.T) {
	firstRound := proposalList{
		Proposal{0x11},
		Proposal{0x22},
		Proposal{0x33},
		Proposal{0x44},
		Proposal{0x55},
		Proposal{0x66},
		Proposal{0x77},
		Proposal{0x88},
		Proposal{0x99},
		Proposal{0x00},
	}

	currentRound := allVotes{
		support: proposalSet{
			firstRound[0]: {},
			firstRound[2]: {},
			firstRound[4]: {},
			firstRound[6]: {},
			firstRound[8]: {},
		},
		against: proposalSet{
			firstRound[1]: {},
			firstRound[3]: {},
			firstRound[5]: {},
			firstRound[7]: {},
			firstRound[9]: {},
		},
	}

	// needs two bytes to encode the votes
	bitVector := []byte{1, 85} // [00000001][01010101]
	result := encodeVotes(currentRound, firstRound)
	assert.Equal(t, bitVector, result)

	got := decodeVotes(bitVector, firstRound)
	assert.Equal(t, currentRound, got)
}

func Test_codec_lessThanActualSize(t *testing.T) {
	firstRound := proposalList{
		Proposal{0x11},
		Proposal{0x22},
		Proposal{0x33},
		Proposal{0x44},
		Proposal{0x55},
		Proposal{0x66},
		Proposal{0x77},
		Proposal{0x88},
		Proposal{0x99},
		Proposal{0x00},
	}

	currentRound := allVotes{
		support: proposalSet{
			firstRound[0]: {},
			firstRound[1]: {},
			firstRound[2]: {},
			firstRound[3]: {},
			firstRound[4]: {},
			firstRound[5]: {},
			firstRound[6]: {},
			firstRound[7]: {},
		},
		against: proposalSet{
			firstRound[8]: {},
			firstRound[9]: {},
		},
	}

	// only needs 1 byte to encode the votes, despite 10 proposals
	bitVector := []byte{255} // [11111111]
	result := encodeVotes(currentRound, firstRound)
	assert.Equal(t, bitVector, result)

	got := decodeVotes(bitVector, firstRound)
	assert.Equal(t, currentRound, got)
}
