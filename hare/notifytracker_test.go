package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildNotifyMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetLayer(*setId1).SetIteration(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	pubKey := generatePubKey(t)

	tracker := NewNotifyTracker(lowDefaultSize)
	exist := tracker.OnNotify(BuildNotifyMsg(pubKey, s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	assert.False(t, exist)
	exist = tracker.OnNotify(BuildNotifyMsg(pubKey, s))
	assert.True(t, exist)
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	s.Add(value3)
	tracker.OnNotify(BuildNotifyMsg(pubKey, s))
	assert.Equal(t, 0, tracker.NotificationsCount(s))
}

func TestNotifyTracker_NotificationsCount(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := NewNotifyTracker(lowDefaultSize)
	tracker.OnNotify(BuildNotifyMsg(generatePubKey(t), s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	tracker.OnNotify(BuildNotifyMsg(generatePubKey(t), s))
	assert.Equal(t, 2, tracker.NotificationsCount(s))
}
