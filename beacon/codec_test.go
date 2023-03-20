package beacon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_codec(t *testing.T) {
	firstRound := []Proposal{
		[4]byte{0x11},
		[4]byte{0x22},
		[4]byte{0x33},
		[4]byte{0x44},
		[4]byte{0x55},
		[4]byte{0x66},
		[4]byte{0x77},
		[4]byte{0x88},
		[4]byte{0x99},
		[4]byte{0x00},
	}

	currentRound := allVotes{
		support: proposalSet{
			firstRound[0].String(): {},
			firstRound[2].String(): {},
			firstRound[4].String(): {},
			firstRound[6].String(): {},
			firstRound[8].String(): {},
		},
		against: proposalSet{
			firstRound[1].String(): {},
			firstRound[3].String(): {},
			firstRound[5].String(): {},
			firstRound[7].String(): {},
			firstRound[9].String(): {},
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
	firstRound := []Proposal{
		[4]byte{0x11},
		[4]byte{0x22},
		[4]byte{0x33},
		[4]byte{0x44},
		[4]byte{0x55},
		[4]byte{0x66},
		[4]byte{0x77},
		[4]byte{0x88},
		[4]byte{0x99},
		[4]byte{0x00},
	}

	currentRound := allVotes{
		support: proposalSet{
			firstRound[0].String(): {},
			firstRound[1].String(): {},
			firstRound[2].String(): {},
			firstRound[3].String(): {},
			firstRound[4].String(): {},
			firstRound[5].String(): {},
			firstRound[6].String(): {},
			firstRound[7].String(): {},
		},
		against: proposalSet{
			firstRound[8].String(): {},
			firstRound[9].String(): {},
		},
	}

	// only needs 1 byte to encode the votes, despite 10 proposals
	bitVector := []byte{255} // [11111111]
	result := encodeVotes(currentRound, firstRound)
	assert.Equal(t, bitVector, result)

	got := decodeVotes(bitVector, firstRound)
	assert.Equal(t, currentRound, got)
}
