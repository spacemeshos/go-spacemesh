package hare

import (
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	k              = 1
	ki             = -1
	lowThresh10    = 10
	lowDefaultSize = 100
)

func genBlockId(i int) types.BlockID {
	buf := make([]byte, binary.MaxVarintLen32)
	n := binary.PutVarint(buf, int64(i))
	return types.NewExistingBlock(types.LayerID(1), buf[:n]).Id()
}

var value1 = genBlockId(1)
var value2 = genBlockId(2)
var value3 = genBlockId(3)
var value4 = genBlockId(4)
var value5 = genBlockId(5)
var value6 = genBlockId(6)
var value7 = genBlockId(7)
var value8 = genBlockId(8)
var value9 = genBlockId(9)
var value10 = genBlockId(10)

func BuildPreRoundMsg(signing Signer, s *Set) *Msg {
	builder := NewMessageBuilder()
	builder.SetType(pre).SetInstanceId(instanceId1).SetRoundCounter(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	verifier := generateSigning(t)

	m1 := BuildPreRoundMsg(verifier, s)
	tracker := newPreRoundTracker(lowThresh10, lowThresh10)
	tracker.OnPreRound(m1)
	assert.Equal(t, 1, len(tracker.preRound))      // one msg
	assert.Equal(t, 2, len(tracker.tracker.table)) // two Values
	g, _ := tracker.preRound[verifier.PublicKey().String()]
	assert.True(t, s.Equals(g))
	assert.Equal(t, uint32(1), tracker.tracker.CountStatus(value1))
	nSet := NewSetFromValues(value3, value4)
	m2 := BuildPreRoundMsg(verifier, nSet)
	tracker.OnPreRound(m2)
	h, _ := tracker.preRound[verifier.PublicKey().String()]
	assert.True(t, h.Equals(s.Union(nSet)))

	interSet := NewSetFromValues(value1, value2, value5)
	m3 := BuildPreRoundMsg(verifier, interSet)
	tracker.OnPreRound(m3)
	h, _ = tracker.preRound[verifier.PublicKey().String()]
	assert.True(t, h.Equals(s.Union(nSet).Union(interSet)))
	assert.Equal(t, uint32(1), tracker.tracker.CountStatus(value1))
	assert.Equal(t, uint32(1), tracker.tracker.CountStatus(value2))
	assert.Equal(t, uint32(1), tracker.tracker.CountStatus(value3))
}

func TestPreRoundTracker_CanProveValueAndSet(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	tracker := newPreRoundTracker(lowThresh10, lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		assert.False(t, tracker.CanProveSet(s))
		m1 := BuildPreRoundMsg(generateSigning(t), s)
		tracker.OnPreRound(m1)
	}

	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_UpdateSet(t *testing.T) {
	tracker := newPreRoundTracker(2, 2)
	s1 := NewSetFromValues(value1, value2, value3)
	s2 := NewSetFromValues(value1, value2, value4)
	prMsg1 := BuildPreRoundMsg(generateSigning(t), s1)
	tracker.OnPreRound(prMsg1)
	prMsg2 := BuildPreRoundMsg(generateSigning(t), s2)
	tracker.OnPreRound(prMsg2)
	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.False(t, tracker.CanProveSet(s1))
	assert.False(t, tracker.CanProveSet(s2))
}

func TestPreRoundTracker_OnPreRound2(t *testing.T) {
	tracker := newPreRoundTracker(2, 2)
	s1 := NewSetFromValues(value1)
	verifier := generateSigning(t)
	prMsg1 := BuildPreRoundMsg(verifier, s1)
	tracker.OnPreRound(prMsg1)
	assert.Equal(t, 1, len(tracker.preRound))
	prMsg2 := BuildPreRoundMsg(verifier, s1)
	tracker.OnPreRound(prMsg2)
	assert.Equal(t, 1, len(tracker.preRound))
}

func TestPreRoundTracker_FilterSet(t *testing.T) {
	tracker := newPreRoundTracker(2, 2)
	s1 := NewSetFromValues(value1, value2)
	prMsg1 := BuildPreRoundMsg(generateSigning(t), s1)
	tracker.OnPreRound(prMsg1)
	prMsg2 := BuildPreRoundMsg(generateSigning(t), s1)
	tracker.OnPreRound(prMsg2)
	set := NewSetFromValues(value1, value2, value3)
	tracker.FilterSet(set)
	assert.True(t, set.Equals(s1))
}
