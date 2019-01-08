package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildCommitMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Commit).SetInstanceId(*instanceId1).SetRoundCounter(Round3).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		m := BuildCommitMsg(generatePubKey(t), s)
		tracker.OnCommit(m)
		assert.False(t, tracker.HasEnoughCommits())
	}

	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.True(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(2, 2, s)
	pub := generatePubKey(t)
	assert.Equal(t, 0, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(pub, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	assert.Equal(t, 1, len(tracker.commits))
	tracker.OnCommit(BuildCommitMsg(pub, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.Equal(t, 2, len(tracker.seenSenders))
}

func TestCommitTracker_HasEnoughCommits(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(2, 2, s)
	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.True(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_BuildCertificate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(2, 2, s)
	assert.Nil(t, tracker.BuildCertificate())
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	cert := tracker.BuildCertificate()
	assert.Equal(t, 2, len(cert.AggMsgs.Messages))
}