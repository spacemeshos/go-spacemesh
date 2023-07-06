package hare3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultThresholdGradedGossiper(t *testing.T) {
	var threshold uint16 = 2
	t.Run("", func(t *testing.T) {
		// Check that with not enough votes we do not reach the threshold.
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, values3, rPre, r0, d)
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
		tgg.ReceiveMsg(id2, values3, rPre, r0, d)
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that with enough good votes we reach the threshold for all values in values.
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, values3, rPre, r0, d)
		tgg.ReceiveMsg(id2, values3, rPre, r0, d)
		tgg.ReceiveMsg(id3, values3, rPre, r0, d)
		assert.ElementsMatch(t, values3, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that with enough good votes we reach the threshold for just val1
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, values3, rPre, r0, d)
		tgg.ReceiveMsg(id2, values3, rPre, r0, d)
		tgg.ReceiveMsg(id3, values1, rPre, r0, d)
		assert.ElementsMatch(t, values1, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that we reach the threshold with just one good vote and remaining malicious votes
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, values3, rPre, r0, d)
		tgg.ReceiveMsg(id2, nil, rPre, r0, d)
		tgg.ReceiveMsg(id3, nil, rPre, r0, d)
		assert.ElementsMatch(t, values3, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that no threshold is reached with just malicious votes
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, nil, rPre, r0, d)
		tgg.ReceiveMsg(id2, nil, rPre, r0, d)
		tgg.ReceiveMsg(id3, nil, rPre, r0, d)
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that a node that equivocates has it's good votes removed.
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		// id1 will vote for values but then equivocate, which should remove their vote for values, but they will still have a malicious vote.
		tgg.ReceiveMsg(id1, values3, rPre, r0, d)
		tgg.ReceiveMsg(id1, nil, rPre, r0, d)
		tgg.ReceiveMsg(id2, values3, rPre, r0, d)
		// At this point we should have one vote for values and one malicious not enough to reach any threshold.
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
		// With a vote for justVal1 we should see justVal1 pass the threshold.
		tgg.ReceiveMsg(id3, values1, rPre, r0, d)
		assert.ElementsMatch(t, values1, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that no threshold is reached with just malicious votes
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, nil, rPre, r0, d)
		tgg.ReceiveMsg(id2, nil, rPre, r0, d)
		tgg.ReceiveMsg(id3, nil, rPre, r0, d)
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that good messages for different rounds are segregated.
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, values3, rPre, r2, d)
		tgg.ReceiveMsg(id2, values3, r0, r2, d)
		tgg.ReceiveMsg(id2, values3, r0, r2, d)
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	})

	t.Run("", func(t *testing.T) {
		// Check that malicious messages for different rounds are segregated.
		tgg := NewDefaultThresholdGradedGossiper(threshold)
		tgg.ReceiveMsg(id1, values3, rPre, r0, d)
		tgg.ReceiveMsg(id2, nil, r0, r1, d)
		tgg.ReceiveMsg(id2, nil, r0, r1, d)
		assert.Empty(t, tgg.RetrieveThresholdMessages(rPre, 1))
	})
}
