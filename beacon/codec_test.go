package beacon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_codec(t *testing.T) {
	firstRound := []Proposal{
		{Value: []byte{0x11}},
		{Value: []byte{0x22}},
		{Value: []byte{0x33}},
		{Value: []byte{0x44}},
		{Value: []byte{0x55}},
		{Value: []byte{0x66}},
		{Value: []byte{0x77}},
		{Value: []byte{0x88}},
		{Value: []byte{0x99}},
		{Value: []byte{0x00}},
	}

	currentRound := allVotes{
		support: proposalSet{
			string([]byte{0x11}): {},
			string([]byte{0x33}): {},
			string([]byte{0x55}): {},
			string([]byte{0x77}): {},
			string([]byte{0x99}): {},
		},
		against: proposalSet{
			string([]byte{0x22}): {},
			string([]byte{0x44}): {},
			string([]byte{0x66}): {},
			string([]byte{0x88}): {},
			string([]byte{0x00}): {},
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
		{Value: []byte{0x11}},
		{Value: []byte{0x22}},
		{Value: []byte{0x33}},
		{Value: []byte{0x44}},
		{Value: []byte{0x55}},
		{Value: []byte{0x66}},
		{Value: []byte{0x77}},
		{Value: []byte{0x88}},
		{Value: []byte{0x99}},
		{Value: []byte{0x00}},
	}

	currentRound := allVotes{
		support: proposalSet{
			string([]byte{0x11}): {},
			string([]byte{0x22}): {},
			string([]byte{0x33}): {},
			string([]byte{0x44}): {},
			string([]byte{0x55}): {},
			string([]byte{0x66}): {},
			string([]byte{0x77}): {},
			string([]byte{0x88}): {},
		},
		against: proposalSet{
			string([]byte{0x99}): {},
			string([]byte{0x00}): {},
		},
	}

	// only needs 1 byte to encode the votes, despite 10 proposals
	bitVector := []byte{255} // [11111111]
	result := encodeVotes(currentRound, firstRound)
	assert.Equal(t, bitVector, result)

	got := decodeVotes(bitVector, firstRound)
	assert.Equal(t, currentRound, got)
}
