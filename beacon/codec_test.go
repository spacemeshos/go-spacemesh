package beacon

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/util"
)

func Test_codec(t *testing.T) {
	props := proposals{
		valid: [][]byte{
			util.Hex2Bytes("11"),
			util.Hex2Bytes("22"),
		},
		potentiallyValid: [][]byte{
			util.Hex2Bytes("33"),
		},
	}
	firstRound := proposals{
		valid: [][]byte{
			util.Hex2Bytes("11"),
			util.Hex2Bytes("22"),
		},
		potentiallyValid: [][]byte{
			util.Hex2Bytes("33"),
		},
	}
	currentRound := allVotes{
		valid: proposalSet{
			string(util.Hex2Bytes("11")): {},
			string(util.Hex2Bytes("33")): {},
		},
		invalid: proposalSet{
			string(util.Hex2Bytes("22")): {},
		},
	}

	bitVector := []uint64{0b101}

	result := encodeVotes(currentRound, props, 100)
	assert.EqualValues(t, bitVector, result)

	original := decodeVotes(bitVector, firstRound)
	assert.EqualValues(t, currentRound, original)
}
