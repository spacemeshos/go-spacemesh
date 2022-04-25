package beacon

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/util"
)

func Test_codec(t *testing.T) {
	firstRound := [][]byte{
		util.Hex2Bytes("11"),
		util.Hex2Bytes("22"),
		util.Hex2Bytes("33"),
		util.Hex2Bytes("44"),
		util.Hex2Bytes("55"),
		util.Hex2Bytes("66"),
		util.Hex2Bytes("77"),
		util.Hex2Bytes("88"),
		util.Hex2Bytes("99"),
		util.Hex2Bytes("00"),
	}

	currentRound := allVotes{
		support: proposalSet{
			string(util.Hex2Bytes("11")): {},
			string(util.Hex2Bytes("33")): {},
			string(util.Hex2Bytes("55")): {},
			string(util.Hex2Bytes("77")): {},
			string(util.Hex2Bytes("99")): {},
		},
		against: proposalSet{
			string(util.Hex2Bytes("22")): {},
			string(util.Hex2Bytes("44")): {},
			string(util.Hex2Bytes("66")): {},
			string(util.Hex2Bytes("88")): {},
			string(util.Hex2Bytes("00")): {},
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
	firstRound := [][]byte{
		util.Hex2Bytes("11"),
		util.Hex2Bytes("22"),
		util.Hex2Bytes("33"),
		util.Hex2Bytes("44"),
		util.Hex2Bytes("55"),
		util.Hex2Bytes("66"),
		util.Hex2Bytes("77"),
		util.Hex2Bytes("88"),
		util.Hex2Bytes("99"),
		util.Hex2Bytes("00"),
	}

	currentRound := allVotes{
		support: proposalSet{
			string(util.Hex2Bytes("11")): {},
			string(util.Hex2Bytes("22")): {},
			string(util.Hex2Bytes("33")): {},
			string(util.Hex2Bytes("44")): {},
			string(util.Hex2Bytes("55")): {},
			string(util.Hex2Bytes("66")): {},
			string(util.Hex2Bytes("77")): {},
			string(util.Hex2Bytes("88")): {},
		},
		against: proposalSet{
			string(util.Hex2Bytes("99")): {},
			string(util.Hex2Bytes("00")): {},
		},
	}

	// only needs 1 byte to encode the votes, despite 10 proposals
	bitVector := []byte{255} // [11111111]
	result := encodeVotes(currentRound, firstRound)
	assert.Equal(t, bitVector, result)

	got := decodeVotes(bitVector, firstRound)
	assert.Equal(t, currentRound, got)
}
