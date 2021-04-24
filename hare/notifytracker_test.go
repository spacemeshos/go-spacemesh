package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildNotifyMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(notify).SetInstanceID(instanceID1).SetRoundCounter(notifyRound).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)
	cert := &certificate{}
	cert.Values = NewSetFromValues(value1).ToSlice()
	cert.AggMsgs = &aggregatedMessages{}
	cert.AggMsgs.Messages = []*Message{BuildCommitMsg(signing, s).Message}
	builder.SetCertificate(cert)
	builder.SetEligibilityCount(1)

	return builder.Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	verifier := generateSigning(t)

	tracker := newNotifyTracker(lowDefaultSize)
	exist := tracker.OnNotify(BuildNotifyMsg(verifier, s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	assert.False(t, exist)
	exist = tracker.OnNotify(BuildNotifyMsg(verifier, s))
	assert.True(t, exist)
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	s.Add(value3)
	tracker.OnNotify(BuildNotifyMsg(verifier, s))
	assert.Equal(t, 0, tracker.NotificationsCount(s))
}

func TestNotifyTracker_NotificationsCount(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := newNotifyTracker(lowDefaultSize)
	tracker.OnNotify(BuildNotifyMsg(generateSigning(t), s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	tracker.OnNotify(BuildNotifyMsg(generateSigning(t), s))
	assert.Equal(t, 2, tracker.NotificationsCount(s))
}
