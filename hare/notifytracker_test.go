package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

const size100 = 100

func BuildNotifyMsg(t *testing.T, pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetLayer(*Layer1).SetIteration(k).SetKi(ki).SetBlocks(s)
	builder, err := builder.SetPubKey(pubKey).Sign(NewMockSigning())
	assert.Nil(t, err)

	return builder.Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	s.Add(blockId2)
	pubKey := generatePubKey(t)

	tracker := NewNotifyTracker(size100)
	exist := tracker.OnNotify(BuildNotifyMsg(t, pubKey, s))
	assert.Equal(t, uint32(1), tracker.NotificationsCount(s))
	assert.False(t, exist)
	exist = tracker.OnNotify(BuildNotifyMsg(t, pubKey, s))
	assert.True(t, exist)
	assert.Equal(t, uint32(1), tracker.NotificationsCount(s))
	s.Add(blockId3)
	tracker.OnNotify(BuildNotifyMsg(t, pubKey, s))
	assert.Equal(t, uint32(0), tracker.NotificationsCount(s))
}

func TestNotifyTracker_NotificationsCount(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	tracker := NewNotifyTracker(size100)
	tracker.OnNotify(BuildNotifyMsg(t, generatePubKey(t), s))
	assert.Equal(t, uint32(1), tracker.NotificationsCount(s))
	tracker.OnNotify(BuildNotifyMsg(t, generatePubKey(t), s))
	assert.Equal(t, uint32(2), tracker.NotificationsCount(s))
}
