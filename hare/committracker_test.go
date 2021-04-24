package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildCommitMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(commit).SetInstanceID(instanceID1).SetRoundCounter(commitRound).SetKi(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		m := BuildCommitMsg(generateSigning(t), s)
		tracker.OnCommit(m)
		assert.False(t, tracker.HasEnoughCommits())
	}

	tracker.OnCommit(BuildCommitMsg(generateSigning(t), s))
	assert.True(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(2, 2, s)
	verifier := generateSigning(t)
	assert.Equal(t, 0, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(verifier, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	assert.Equal(t, 1, len(tracker.commits))
	tracker.OnCommit(BuildCommitMsg(verifier, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(generateSigning(t), s))
	assert.Equal(t, 2, len(tracker.seenSenders))
}

func TestCommitTracker_HasEnoughCommits(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(2, 2, s)
	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(BuildCommitMsg(generateSigning(t), s))
	tracker.OnCommit(BuildCommitMsg(generateSigning(t), s))
	assert.True(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_BuildCertificate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(2, 2, s)
	assert.Nil(t, tracker.BuildCertificate())
	tracker.OnCommit(BuildCommitMsg(generateSigning(t), s))
	tracker.OnCommit(BuildCommitMsg(generateSigning(t), s))
	cert := tracker.BuildCertificate()
	assert.Equal(t, 2, len(cert.AggMsgs.Messages))
	assert.Nil(t, cert.AggMsgs.Messages[0].InnerMsg.Values)
	assert.Nil(t, cert.AggMsgs.Messages[1].InnerMsg.Values)
}
