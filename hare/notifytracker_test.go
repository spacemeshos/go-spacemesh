package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildNotifyMsg(signing Signing, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetInstanceId(*instanceId1).SetRoundCounter(Round4).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(signing.Verifier().Bytes()).Sign(signing)
	cert := &pb.Certificate{}
	cert.Values = NewSetFromValues(value1).To2DSlice()
	cert.AggMsgs = &pb.AggregatedMessages{}
	cert.AggMsgs.Messages = []*pb.HareMessage{BuildCommitMsg(signing, s)}
	builder.SetCertificate(cert)

	return builder.Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	verifier := generateSigning(t)

	tracker := NewNotifyTracker(lowDefaultSize)
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
	tracker := NewNotifyTracker(lowDefaultSize)
	tracker.OnNotify(BuildNotifyMsg(generateSigning(t), s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	tracker.OnNotify(BuildNotifyMsg(generateSigning(t), s))
	assert.Equal(t, 2, tracker.NotificationsCount(s))
}
