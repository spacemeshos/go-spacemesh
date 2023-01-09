package hare

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func BuildNotifyMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(notify).SetLayer(instanceID1).SetRoundCounter(notifyRound).SetCommittedRound(ki).SetValues(s)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)
	cert := &Certificate{}
	cert.Values = NewSetFromValues(value1).ToSlice()
	cert.AggMsgs = &AggregatedMessages{}
	cert.AggMsgs.Messages = []Message{BuildCommitMsg(signing, s).Message}
	builder.SetCertificate(cert)
	builder.SetEligibilityCount(1)

	return builder.Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	signer, err := signing.NewEdSigner()
	assert.NoError(t, err)

	tracker := newNotifyTracker(lowDefaultSize)
	exist := tracker.OnNotify(BuildNotifyMsg(signer, s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	assert.False(t, exist)
	exist = tracker.OnNotify(BuildNotifyMsg(signer, s))
	assert.True(t, exist)
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	s.Add(value3)
	tracker.OnNotify(BuildNotifyMsg(signer, s))
	assert.Equal(t, 0, tracker.NotificationsCount(s))
}

func TestNotifyTracker_NotificationsCount(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	assert.NoError(t, err)

	signer2, err := signing.NewEdSigner()
	assert.NoError(t, err)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := newNotifyTracker(lowDefaultSize)
	tracker.OnNotify(BuildNotifyMsg(signer1, s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	tracker.OnNotify(BuildNotifyMsg(signer2, s))
	assert.Equal(t, 2, tracker.NotificationsCount(s))
}
