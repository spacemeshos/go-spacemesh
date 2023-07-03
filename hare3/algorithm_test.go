package hare3

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	id1 = randHash20()
	id2 = randHash20()
	id3 = randHash20()

	val1 = randHash20()
	val2 = randHash20()
	val3 = randHash20()

	rPre = NewAbsRound(0, -1)
	r0   = NewAbsRound(0, 0)
	r1   = NewAbsRound(0, 1)
	r2   = NewAbsRound(0, 2)
)

func TestDefaultThresholdGradedGossiper(t *testing.T) {
	var threshold uint16 = 2
	tgg := NewDefaultThresholdGradedGossiper(threshold)

	values := []types.Hash20{val1, val2, val3}

	tgg.ReceiveMsg(id1, values, rPre, r0, d)
	assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	tgg.ReceiveMsg(id2, values, rPre, r0, d)
	assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	tgg.ReceiveMsg(id3, values, rPre, r0, d)
	assert.Equal(t, values, tgg.RetrieveThresholdMessages(rPre, 1))
}

func randHash20() types.Hash20 {
	var result types.Hash20
	rand.Read(result[:])
	return result
}
