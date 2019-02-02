package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	k           = 1
	ki          = -1
	lowThresh10 = 10
	lowDefaultSize = 100
)

var value1 = Value{Bytes32{1}}
var value2 = Value{Bytes32{2}}
var value3 = Value{Bytes32{3}}
var value4 = Value{Bytes32{4}}
var value5 = Value{Bytes32{5}}
var value6 = Value{Bytes32{6}}
var value7 = Value{Bytes32{7}}
var value8 = Value{Bytes32{8}}
var value9 = Value{Bytes32{9}}
var value10 = Value{Bytes32{10}}



func BuildPreRoundMsg(verifier Verifier, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetInstanceId(*instanceId1).SetRoundCounter(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(verifier.Bytes()).Sign(NewMockSigning())

	return builder.Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	verifier := generateVerifier(t)

	m1 := BuildPreRoundMsg(verifier, s)
	tracker := NewPreRoundTracker(lowThresh10, lowThresh10)
	tracker.OnPreRound(m1)
	assert.Equal(t, 1, len(tracker.preRound))      // one msg
	assert.Equal(t, 2, len(tracker.tracker.table)) // two values
	_, exist1 := tracker.preRound[verifier.String()]
	m2 := BuildPreRoundMsg(verifier, s)
	tracker.OnPreRound(m2)
	_, exist2 := tracker.preRound[verifier.String()]
	assert.Equal(t, exist1, exist2) // same pub --> same msg
}

func TestPreRoundTracker_CanProveValueAndSet(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	tracker := NewPreRoundTracker(lowThresh10, lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		assert.False(t, tracker.CanProveSet(s))
		m1 := BuildPreRoundMsg(generateVerifier(t), s)
		tracker.OnPreRound(m1)
	}

	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_UpdateSet(t *testing.T) {
	tracker := NewPreRoundTracker(2, 2)
	s1 := NewSetFromValues(value1, value2, value3)
	s2 := NewSetFromValues(value1, value2, value4)
	prMsg1 := BuildPreRoundMsg(generateVerifier(t), s1)
	tracker.OnPreRound(prMsg1)
	prMsg2 := BuildPreRoundMsg(generateVerifier(t), s2)
	tracker.OnPreRound(prMsg2)
	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.False(t, tracker.CanProveSet(s1))
	assert.False(t, tracker.CanProveSet(s2))
}

func TestPreRoundTracker_OnPreRound2(t *testing.T) {
	tracker := NewPreRoundTracker(2, 2)
	s1 := NewSetFromValues(value1)
	verifier := generateVerifier(t)
	prMsg1 := BuildPreRoundMsg(verifier, s1)
	tracker.OnPreRound(prMsg1)
	assert.Equal(t, 1, len(tracker.preRound))
	prMsg2 := BuildPreRoundMsg(verifier, s1)
	tracker.OnPreRound(prMsg2)
	assert.Equal(t, 1, len(tracker.preRound))
}