package hare

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	k              = 1
	ki             = -1
	lowThresh10    = 10
	lowDefaultSize = 100
)

func genBlockID(i int) types.BlockID {
	return types.NewExistingBlock(types.LayerID(1), util.Uint32ToBytes(uint32(i)), nil).ID()
}

var value1 = genBlockID(1)
var value2 = genBlockID(2)
var value3 = genBlockID(3)
var value4 = genBlockID(4)
var value5 = genBlockID(5)
var value6 = genBlockID(6)
var value7 = genBlockID(7)
var value8 = genBlockID(8)
var value9 = genBlockID(9)
var value10 = genBlockID(10)

func BuildPreRoundMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(pre).SetInstanceID(instanceID1).SetRoundCounter(k).SetKi(ki).SetValues(s)
	builder.SetPubKey(signing.PublicKey())
	builder.SetEligibilityCount(1)

	return builder.Sign(signing).Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	verifier := generateSigning(t)

	m1 := BuildPreRoundMsg(verifier, s)
	tracker := newPreRoundTracker(lowThresh10, lowThresh10, log.AppLog)
	tracker.OnPreRound(context.TODO(), m1)
	assert.Equal(t, 1, len(tracker.preRound))      // one msg
	assert.Equal(t, 2, len(tracker.tracker.table)) // two Values
	g, _ := tracker.preRound[verifier.PublicKey().String()]
	assert.True(t, s.Equals(g))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value1))
	nSet := NewSetFromValues(value3, value4)
	m2 := BuildPreRoundMsg(verifier, nSet)
	m2.InnerMsg.EligibilityCount = 2
	tracker.OnPreRound(context.TODO(), m2)
	h, _ := tracker.preRound[verifier.PublicKey().String()]
	assert.True(t, h.Equals(s.Union(nSet)))

	interSet := NewSetFromValues(value1, value2, value5)
	m3 := BuildPreRoundMsg(verifier, interSet)
	tracker.OnPreRound(context.TODO(), m3)
	h, _ = tracker.preRound[verifier.PublicKey().String()]
	assert.True(t, h.Equals(s.Union(nSet).Union(interSet)))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value1))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value2))
	assert.EqualValues(t, 2, tracker.tracker.CountStatus(value3))
	assert.EqualValues(t, 2, tracker.tracker.CountStatus(value4))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value5))
}

func TestPreRoundTracker_CanProveValueAndSet(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	tracker := newPreRoundTracker(lowThresh10, lowThresh10, log.AppLog)

	for i := 0; i < lowThresh10; i++ {
		assert.False(t, tracker.CanProveSet(s))
		m1 := BuildPreRoundMsg(generateSigning(t), s)
		tracker.OnPreRound(context.TODO(), m1)
	}

	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_UpdateSet(t *testing.T) {
	tracker := newPreRoundTracker(2, 2, log.AppLog)
	s1 := NewSetFromValues(value1, value2, value3)
	s2 := NewSetFromValues(value1, value2, value4)
	prMsg1 := BuildPreRoundMsg(generateSigning(t), s1)
	tracker.OnPreRound(context.TODO(), prMsg1)
	prMsg2 := BuildPreRoundMsg(generateSigning(t), s2)
	tracker.OnPreRound(context.TODO(), prMsg2)
	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.False(t, tracker.CanProveSet(s1))
	assert.False(t, tracker.CanProveSet(s2))
}

func TestPreRoundTracker_OnPreRound2(t *testing.T) {
	tracker := newPreRoundTracker(2, 2, log.AppLog)
	s1 := NewSetFromValues(value1)
	verifier := generateSigning(t)
	prMsg1 := BuildPreRoundMsg(verifier, s1)
	tracker.OnPreRound(context.TODO(), prMsg1)
	assert.Equal(t, 1, len(tracker.preRound))
	prMsg2 := BuildPreRoundMsg(verifier, s1)
	tracker.OnPreRound(context.TODO(), prMsg2)
	assert.Equal(t, 1, len(tracker.preRound))
}

func TestPreRoundTracker_FilterSet(t *testing.T) {
	tracker := newPreRoundTracker(2, 2, log.AppLog)
	s1 := NewSetFromValues(value1, value2)
	prMsg1 := BuildPreRoundMsg(generateSigning(t), s1)
	tracker.OnPreRound(context.TODO(), prMsg1)
	prMsg2 := BuildPreRoundMsg(generateSigning(t), s1)
	tracker.OnPreRound(context.TODO(), prMsg2)
	set := NewSetFromValues(value1, value2, value3)
	tracker.FilterSet(set)
	assert.True(t, set.Equals(s1))
}
