package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
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

func BuildPreRoundMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetInstanceId(*instanceId1).SetRoundCounter(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	pubKey := generatePubKey(t)

	m1 := BuildPreRoundMsg(pubKey, s)
	tracker := NewPreRoundTracker(lowThresh10, lowThresh10)
	tracker.OnPreRound(m1)
	assert.Equal(t, 1, len(tracker.preRound))      // one msg
	assert.Equal(t, 2, len(tracker.tracker.table)) // two values
	_, exist1 := tracker.preRound[pubKey.String()]
	m2 := BuildPreRoundMsg(pubKey, s)
	tracker.OnPreRound(m2)
	_, exist2 := tracker.preRound[pubKey.String()]
	assert.Equal(t, exist1, exist2) // same pub --> same msg
}

func TestPreRoundTracker_CanProveValueAndSet(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	tracker := NewPreRoundTracker(lowThresh10, lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		assert.False(t, tracker.CanProveSet(s))
		m1 := BuildPreRoundMsg(generatePubKey(t), s)
		tracker.OnPreRound(m1)
	}

	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.True(t, tracker.CanProveSet(s))
}
