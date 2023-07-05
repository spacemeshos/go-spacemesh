package hare3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGradecast(t *testing.T) {
	// Grade 2, received same round as message with max grade.
	gc := NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r0, 3)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 2}}, gc.RetrieveGradecastedMessages(r0))

	// Grade 2, received 1 round after message round with max grade.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 2}}, gc.RetrieveGradecastedMessages(r0))

	// Grade 1, received 2 rounds after message round with max grade.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r2, 3)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 1}}, gc.RetrieveGradecastedMessages(r0))

	// No result, received 3 rounds after message round with max grade.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r3, 3)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 4 rounds after message round with max grade.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r4, 3)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// Grade 1, received same round as message with grade 2.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r0, 2)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 1}}, gc.RetrieveGradecastedMessages(r0))

	// Grade 1, received 1 round after message round with grade 2.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 2)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 1}}, gc.RetrieveGradecastedMessages(r0))

	// Grade 1, received 2 rounds after message round with grade 2.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r2, 2)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 1}}, gc.RetrieveGradecastedMessages(r0))

	// No result, received 3 rounds after message round with grade 2.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r3, 2)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 4 rounds after message round with grade 2.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r4, 2)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 1 round after message round with grade 1.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 1)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 2 rounds after message round with grade 1.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r2, 1)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 3 rounds after message round with grade 1.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r3, 1)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 1 round after message round with max grade with
	// subsequent malicious message 1 round after message round.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id1, nil, r0, r1, 3)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 1 round after message round with max grade with
	// subsequent malicious message 2 rounds after message round.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id1, nil, r0, r2, 3)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// No result, received 1 round after message round with max grade with
	// subsequent malicious message 3 rounds after message round.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id1, nil, r0, r3, 3)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// Grade 2, received 1 round after message round with max grade with
	// subsequent malicious message 4 rounds after message round.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id1, nil, r0, r4, 3)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 2}}, gc.RetrieveGradecastedMessages(r0))

	// No result, received 2 rounds after message round with max grade with
	// subsequent malicious message 2 rounds after message round.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r2, 3)
	gc.ReceiveMsg(id1, nil, r0, r2, 3)
	assert.Nil(t, gc.RetrieveGradecastedMessages(r0))

	// Grade 1, received 2 rounds after message round with max grade with
	// subsequent malicious message 3 rounds after message round.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r2, 3)
	gc.ReceiveMsg(id1, nil, r0, r3, 3)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 1}}, gc.RetrieveGradecastedMessages(r0))

	// Multiple messages from multiple identities.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id2, values2, r0, r1, 3)
	gc.ReceiveMsg(id3, values1, r0, r1, 3)
	expect := []GradecastedSet{{id1, values3, 2}, {id2, values2, 2}, {id3, values1, 2}}
	assert.ElementsMatch(t, expect, gc.RetrieveGradecastedMessages(r0))

	// Multiple messages from multiple identities with multiple malicious messages.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id2, values2, r0, r2, 3)
	gc.ReceiveMsg(id3, values1, r0, r2, 2)
	gc.ReceiveMsg(id1, nil, r0, r3, 2) // should negate message from id1
	gc.ReceiveMsg(id2, nil, r0, r2, 3) // should negate message from id2
	gc.ReceiveMsg(id2, nil, r0, r3, 2) // should not negate message from id3
	expect = []GradecastedSet{{id3, values1, 1}}
	assert.ElementsMatch(t, expect, gc.RetrieveGradecastedMessages(r0))

	// Messages from different rounds are segregated.
	gc = NewGradecaster()
	gc.ReceiveMsg(id1, values3, r0, r1, 3)
	gc.ReceiveMsg(id1, values2, r1, r2, 3)
	gc.ReceiveMsg(id1, values1, r2, r3, 3)
	assert.ElementsMatch(t, []GradecastedSet{{id1, values3, 2}}, gc.RetrieveGradecastedMessages(r0))
	assert.ElementsMatch(t, []GradecastedSet{{id1, values2, 2}}, gc.RetrieveGradecastedMessages(r1))
	assert.ElementsMatch(t, []GradecastedSet{{id1, values1, 2}}, gc.RetrieveGradecastedMessages(r2))
}
